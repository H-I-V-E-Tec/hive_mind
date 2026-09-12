---
program_id: coinspot
document_type: note
claimed_scope_status: unknown
classification: internal
source: hackerone policy (coinspot) — CSV export 2026-09-09
collected_at: 2026-09-12T00:00:00Z
tags: [coinspot, root]
asset_refs: [www.coinspot.com.au]
---
# CoinSpot — ficha de alvo

- **Programa:** CoinSpot (coinspot) · HackerOne · maior exchange cripto da Austrália · Gold Standard Safe Harbor
- **Link:** https://hackerone.com/coinspot · API: https://www.coinspot.com.au/v2/api
- **Prioridade:** 🟢 **ALTA — candidata a #1** (menor concorrência 54/90d + maiores pisos + fit direto)
- **Escopo:** closed, 4 assets. coinspot.com.au. API v2.
- **Reward (mínimos):** Low $1.000 · Med $2.000 · High $10.000 · Crit $50.000 · **especial $350.000** ("new, high impact weaknesses affecting the platform or **underlying technology**" — a classe HMAC/canonicalização se encaixa).
- **Test Plan:** 2 contas grátis; a registrada com `<h1user>@wearehackerone.com` = **acesso de teste completo (sem KYC)**. API doc: coinspot.com.au/v2/api.

## 🎚️ Rubrica de severidade (da policy) — calibra os leads
| Tier | Exemplos citados | Nossos leads |
|---|---|---|
| **Critical** | "Access to funds (digital or fiat) **outside the context of a given user**", "Mass unauthorised access/modification of sensitive data", RCE | **C1** (dup-key smuggling no saque → valor/destino não-assinado) |
| **High** | "**False top-up** vulnerabilities in crypto deposit processes", "**Manipulation of a single user's account balances**", "Stored/reflected XSS **without user interaction**" | `/my/coin/deposit` coin/network confusion (lead nível-1); race no nonce (P6); blind XSS `/ua` (P17, **se disparar**) |
| **Medium** | "Disclosure of current user account/wallet balances", XSS **com** interação | vazamento de saldo via IDOR de leitura |
| **Low** | "require unlikely circumstances", "**locking a valid user out**" | nonce-exhaustion DoS (escalada do P1) = só Low |

## 🚫 OOS relevante (da policy)
- **"Reports of publicly leaked account credentials"** → **rebaixa chain C3** (key vazada no GitHub = OOS por si só; só vale se demonstrar impacto novo do *design* sem-2FA).
- Rate-limit/brute em **endpoint não-auth** = OOS (mas em endpoint de **auth** não é auto-OOS — só "distributed brute force contra password/TOTP" é excluído de High).
- "Enablement of 2FA without email confirmation" = known issue, pular.
- "Upload de arquivo malicioso via **S3 pre-signed URL** sem impacto demonstrável" = OOS → mas **implica que há S3 pre-signed URLs** em algum fluxo (verificação/depósito?) — anotar como superfície.
- Libs vulneráveis sem PoC · browsers desatualizados · 0-day com patch < 1 mês.

## ⛓️ Constraints pré-fixadas
- **2026-09-08:** `scope.md` preenchido com o escopo real (era template genérico — `scope_hook` não pega template). Verificado: hook bloqueia `coinspot.com`/`.co`/`.io`, libera `*.coinspot.com.au`. **Reconferir na policy viva (privada) antes do Nível 1** — incl. se exige header `X-Bug-Bounty`/handle.
- 2 contas grátis (uma com alias `<h1username>@wearehackerone.com`).
- **🚫 Ataques a contas de CLIENTES são PROIBIDOS** — só entre as suas contas.
- closed scope (4 assets); programa privado; ~5 req/s.
- rate-limit / brute-force não-autenticado = OOS. **Não brutar `/accountrecovery/*`.**

## 🎯 Onde mirar (fit BAC/lógica)
- IDOR/BAC de **saldo/carteira/ordens** entre suas próprias contas.
- **False top-up** / manipulação de saldo (lógica de negócio).
- Disclosure de saldo/dados de usuário; ATO; XSS.
- Testar a **API v2** endpoint a endpoint com as 6 perguntas do The Mind.

---

## Recon — `protocolo ladrão de bancos` (2026-09-02)
`recon.py --all --rate 5` + `cors_headers_scan.py` + OSINT (gau/wayback/doc da API/JS/well-known). Cru em `recon/`.
- subfinder **10** → **5 resolvem** → **2 vivos**: `coinspot.com.au`, `www.coinspot.com.au`. (Outros subs = `o2.comms` / `o3.ptr1105` / `o4.ptr7513` = infra de e-mail, OOS.)
- **gau: 119.403 URLs** (8,6 MB — corpus histórico rico, minerado à mão). `waybackurls`: 222. `katana`: **timeout 600s** (Cloudflare + site grande). `nuclei`: 0 (fallback de env). `gf`: não instalado → garimpo manual.
- **CORS/headers:** 0 ALTO · 0 MÉDIO · 2 BAIXO (`hsts` sem includeSubDomains; Permissions-Policy ausente) → best-practice/Low, provável Core Ineligible. **Nada a perseguir aí.**
- Front: **Cloudflare** (`__cf_bm`, Bot Management, cf-challenge). CSP boa (`script-src https: 'nonce-…' 'strict-dynamic'`, `base-uri 'none'`, `object-src 'none'`). `_csrf` cookie `HttpOnly; SameSite=Strict`. App server-rendered (nonce por request), não SPA.

### API v2 (o filão) — `https://www.coinspot.com.au`
| Superfície | Base | Método | Auth |
|---|---|---|---|
| Pública | `/pubapi/v2` | GET | nenhuma (testado: `/pubapi/v2/latest/btc` → `{"status":"ok",…}`) |
| Autenticada (full) | `/api/v2` | POST | **HMAC-SHA512**: headers `key` + `sign` (HMAC do body c/ secret) + param `nonce` (inteiro monotônico) |
| Read-Only | `/api/v2/ro` | POST | idem (testado sem cred → `401 {"status":"error","message":"no key"}`) |
- Rate: **1000 req/min**.
- **POST full (seleção):** `/my/coin/deposit` · `/quote/buy/now` `/quote/sell/now` `/quote/swap/now` → `/my/buy/now` `/my/sell/now` `/my/swap/now` · `/my/buy` `/my/buy/edit` `/my/buy/cancel` `/my/buy/cancel/all` (idem sell) · `/my/coin/withdraw/senddetails` `/my/coin/withdraw/send` `/my/coin/withdraw/send/async` `/my/coin/withdraw/send/status`
- **POST read-only:** `/my/balances` `/my/balance/{cointype}` · `/my/orders/market/open` `/my/orders/limit/open` `/my/orders/completed` · `/my/sendreceive` `/my/deposits` `/my/withdrawals` `/my/affiliatepayments` `/my/referralpayments`

### Web autenticado (`/my/*`, de gau — todos `302 → /login` sem sessão)
- **`/my/buycomplete/{ObjectId}`** — confirmação de compra keyed por **Mongo ObjectId** (24 hex, semi-previsível: timestamp+contador). Vários IDs reais indexados no gau.
- **`/my/deposit/pending/paypal/{uuid}`** e **`/my/deposit/pending/card/{uuid}`** — página de depósito pendente por UUID. IDs reais indexados.
- `/my/deposit/{bpay,card,cash,online,payid,paypal,payto,poli}` — múltiplos rails de depósito.
- `/my/orders/eofy/{ano}` · `/my/orders/eofyvalue/{ano}` — **relatório fiscal EOFY por ano** (IDOR por ano/usuário?).
- `/my/api` (gestão de API keys) · `/my/bank` · `/my/affiliate` · `/my/otc` `/my/otc/buy` · `/my/messagecenter/{count,markread,preview,remove}` · `/my/pricealerts/edit` · `/my/order/pegged` · `/my/btcorders/new` · `/my/nft/wallet` `/my/nft/asset/{col}/{contract}/{tokenid}` · `/my/payment` · `/my/dashboard` `/my/myaccount` `/my/newaccount`
- **`/accountrecovery/{2fa,email,mobile,enablewithdrawals,geolockdisable,unlockaccount}`** — cadeia de recuperação. `enablewithdrawals` / `geolockdisable` / `unlockaccount` = **superfície de ATO/lógica**. ⚠️ **não brutar** — só teste lógico com conta própria.
- Universal links (de `apple-app-site-association`): **`/withdrawalfiat/confirmed`** (`?processed=true`, `?expired=true`, `/processed`, `/expired`), `/my/settings/verification`, `/emailconfirmed`.

### Mobile
- Android `com.coinspot.app` (2 cert fingerprints — prod + beta?). iOS `5Q5H52GDRT.com.coinspot.app`. Provavelmente 2 dos 4 assets in-scope.

### OSINT
- `security.txt` → aponta pro H1. `robots.txt` = `Allow: /`. Sem writeups públicos (disclosure restrita).
- crt.sh 502 (retentado 2×). GitHub code search por `coinspot` — **pendente** (sem `gh`; fazer via web depois).

---

## Leads por camada de acesso (unauth → 1 conta → 2 contas)

### Nível 0 — UNAUTH (testado 2026-09-02, `seguir o rastro` — **esgotado**)
Detalhe em `arapuca.md` (peças P1–P19). Resumo do que sobrevive:
- **[SINAL FORTE] P1 — parser do `nonce` = `parseInt(x,10)` + falsy-guard.** Passam `1e30`/`"1e9"`/`"01"`/`-1`/`1.5`/`[1]`; falham `0`/`-0`/`".5"`/`"Infinity"`. → candidato a **diferencial de parser** no único controle anti-replay (P5): se compare/store usa `Number`/raw → replay/exhaustion. Confirmar com 1 key (lead B).
- **[SINAL FORTE] P2 — JSON body = LAST-KEY-WINS** em chave duplicada (confirmado unauth). + **P13** (`sign` = HMAC dos bytes crus, `json.dumps` compacto, não-canonicalizado — dos wrappers) → **chain C1** (dup-key smuggling de `amount`/`address` no saque).
- **[SINAL] P3 — body parseado só com CT `application/json` (match frouxo) ou `x-www-form-urlencoded`** (2 encodings). **P4 — método não enforçado na auth**; `GET` com body JSON parseia `nonce`; só `PUT`/`PATCH` barrados (pela Cloudflare).
- **[SINAL MÉDIO] P17 — `/ua` e `/ua/event`** (de `pages_main.js`): beacon **unauth, sem CSRF, sem throttle**. `title`/`path`/`referrer`/`userAgentOverride`/`label`/`fp` = atacante-controlado, gravado no store de atividade interno → **blind/stored XSS em dashboard de staff** + pré-seed/spoof do `fp` (P12 concreto). Precisa de callback OOB pra confirmar.
- **[SINAL] P14 — `nonce` = timestamp µs** (não contador) → race (check-then-store atômico?) + exhaustion. **P19 — V1 API tem menos guardas**, mesmo esquema de assinatura → reforça **C2** (replay cross-versão).
- **[SINAL Low] P15 — ALTCHA `maxnumber:1000000`** = PoW trivial (mas server-fixed). **P16 — `/forgotpassword` existe**. **P11 — `/accountrecovery/*` 200 unauth** (entrada pública; sem brutar).
- **[FP/fechado]** delay de auth-fail (jitter) · "traversal" pubapi (curl normaliza `../`) · `?code=` reflection (grep casou "pa**ssword**") · `/join` redirect (path fixo, sem CRLF/open-redirect) · ALTCHA `?maxnumber=` client-side · rota que pula middleware (nenhuma) · `coinspot.min.js` (só jQuery) · `csutm` reflection server-side · cache poisoning (nada dinâmico cacheável) · subdomínios (0/~90) · `/withdrawalfiat/confirmed` (cosmética).
- **Bloqueado:** APK (`com.coinspot.app`) — mirrors 403; sem `jadx`/`apktool` local.

### Nível 1 — `protocolo One Piece` (1 conta + API key) — **PRÓXIMA FASE**
| # | Lead | Padrão | Precisa |
|---|------|--------|---------|
| A | **Confirmar chain C1 (dup-key smuggling no saque)** — `sign` cobre bytes crus ou params canônicos? handler usa a última ocorrência de `amount`/`address`? | P2+P3+P5 · arsenal *IDOR dup-key* / *checkout logic* | 1 key full+withdraw (testar c/ valores mínimos, para-e-confirma) |
| B | **Diferencial de parser do nonce (P1)** — enviar `{"nonce":1e15}` e depois `{"nonce":2}`; se o 2º passar, gate (parseInt=1) ≠ store. → replay/exhaustion. | P5 · arsenal *falsy-guard anti-replay* | 1 key RO |
| C | **Race no `nonce`** (check-then-use atômico?) — rajada paralela, mesmo nonce, na própria conta → dupla execução. | P6 · arsenal *race* | 1 key |
| D | **Bypass de escopo RO→write / V1** — key RO da V2 chama endpoint de escrita? funciona na V1 (`/api/my/buy`)? V1 dispensa a checagem de flag RO? | P8/Q3 · arsenal *BAC bypass* | 1 key RO |
| E | **`rate` do cliente no `/my/buy/now`** (P9) — é teto protetor ou preço de execução? `rate` = metade do mercado + `threshold=1000`. | P4 · arsenal *checkout logic* | 1 conta |
| F | **`senddetails`→`send` step-skip** (P7) — pular `senddetails`; `send` re-valida limite/enablement/2FA? `emailconfirm` default NO — a API realmente dispensa 2FA? | P2/P4 · arsenal *checkout logic* | 1 conta (para-e-confirma) |
| G | **`/my/buy/edit` localiza por `rate` ou `id`?** (P10) — mandar `id` da ordem X com `rate` da ordem Y. `newrate`→~0. | P3/P4 | 1 conta |
| H | **`/withdrawalfiat/confirmed` handler real** (P8) — fazer saque fiat, capturar link do e-mail, dissecar o token. | P4/P6 | 1 conta |
| I | **Fingerprint de risco (P12)** — `brhash` alimenta gate de novo-device/geolock/step-up? valor client-controlado? | arsenal *Identity/Anti-abuse bypass* | 1 conta + inspeção de tráfego |
| J | **`/accountrecovery/*` lógica** — fail-open, step-skip, token não-bound. Sem brutar. | P2/P6 | 1 conta |

### Nível 2 — cross-account (2 contas)
| # | Lead | Padrão |
|---|------|--------|
| K | **IDOR real** — objeto/ordem/saldo/relatório da conta B com a key da conta A. `/my/buy/cancel` id de B, `/my/orders/eofy/{ano}` de B, `/my/buycomplete/{ObjectId}` de B (ObjectId semi-previsível). | P1/P3 |
| L | **Quote reuse cross-account** — `rate`/quote favorável obtido na conta A (fee tier / OTC) usado na conta B. | P2 · arsenal *checkout logic* |
| M | **`send/status` `pollid` de B** — GUID v4 (não enumerável), mas se vaza (log/referral) → ver destino+valor de saque alheio; ou agir sobre job alheio. | P3 |
| N | **`/my/deposit/pending/{paypal,card}/{uuid}` de B** — página de depósito pendente por UUID. | P3 |

## Rastro (`seguir o rastro`) — árvore
```
RAIZ: "erro de nonce vem antes do erro de key" (GET vs POST diff)
├─ R1 pipeline order .......... [MAPEADO] nonce-fmt→key-presente→key-existe→sign→escopo→nonce-monotônico→handler
├─ R2 key/sign via query/body . [FECHADO] header-only
├─ R3 content-type ............ [SINAL P3] parse só em application/json (match frouxo) + x-www-form-urlencoded; JSON estrito (lixo→400)
├─ R4 method .................. [SINAL P4] CF bloqueia só PUT/PATCH; GET parseia body JSON
├─ R5 nonce format parser ..... [SINAL FORTE P1] = parseInt(x,10) + if(!v); fingerprint fechado
│    └─ escalada: gate(parseInt) ≠ compare/store(Number/raw) ⇒ replay/exhaustion  → nível 1 lead B
├─ R6 dup-key resolution ...... [SINAL FORTE P2] LAST-KEY-WINS + P13 (sign = bytes crus) → chain C1
├─ R7 auth-fail delay ......... [FP] jitter de rede
│
RAIZ 2: pages_main.js → /ua beacon
├─ R8 /ua contract ........... [SINAL P17] unauth, sem CSRF, sem throttle; campo faltando→500; body cap ~poucos KB
│    ├─ fp: fp.get() enviado unauth → P12 concreto (pré-seed/spoof de device fingerprint)
│    ├─ userAgentOverride → servidor grava UA escolhida pelo cliente
│    └─ blind XSS em dashboard interno (path/referrer/label sem escape) → precisa callback OOB
├─ R9 csutm cookie .......... [FP unauth] Base64(JSON) client-controlled, mas servidor não reflete
└─ R10 uaCookie/csua ........ [Info] "user id" = Base64(timestamp), previsível
```

## Próximo passo
- **Handoff humano (destrava nível 1):** criar 1–2 contas em `www.coinspot.com.au` (uma com alias `<h1username>@wearehackerone.com`); gerar **API key** em `/my/api` — **começar por 1 key read-only** (leads B, D) e depois 1 **full+withdraw** (leads A, C, F — com valores mínimos). Passar `key` + `secret`.
- **Dá pra já sem conta:** GitHub/GitLab code search por keys `coinspot` vazadas (chain C3); baixar o APK `com.coinspot.app` (endpoints/flags); minerar `urls.txt` (params `code`/`token`).
- **Ao ligar nível 1:** `protocolo One Piece` + `formação de lança`, ordem sugerida: **B → D → A/C1** (o mais promissor), depois E/F/G.

## Achados
| Data | Severidade | Tipo | Endpoint | Status |
|------|-----------|------|----------|--------|
|      |           |      |          | _(nenhum achado válido ainda)_ |

## Estado do nível 1 (2026-09-09 — API v2, 1 conta)
Camada de **auth/lógica da API v2 exaurida sem achado** (key RO + key full/trade; withdraw NÃO habilitado). Ver `brain.py brief coinspot` e `arapuca.md` (modelo do sistema + C5–C9).

| Lead/Chain | Veredito | Nota |
|---|---|---|
| ⭐ C1 dup-key smuggling no saque | ❌ **REFUTADA** | server assina `canonical(parse(body))`, não bytes crus (P20) |
| B parser do nonce | ❌ negativo | `parseInt`-leniente mas store/compare numéricos |
| C2 replay cross-versão | ❌ negativo | nonce = contador global por key (V1↔V2) |
| C5 race no nonce | ❌ negativo | gate check-then-store **atômico** (1/30 burst) |
| D escopo RO→write / V1 | ❌ negativo | key-type check pré-dispatch, V1 e V2 |
| quote tampering | ❌ negativo | `amount`≤0 / `amounttype` inválido → 400 |
| C6 `rate`=execução? (limit) | ❌ negativo | `rate` é limite/proteção; 0 AUD a 2× mercado |
| G/H-E `sell/edit` | ❌ negativo | valida rate atual + `newrate`>0 (fail-closed) |
| C9 deposit chain-confusion | 🟡 dormant | mislabel `BSC→network:"ETH"`; precisa ver crédito |

**Ainda abertos (precisam de acesso):** **F/P7/H-G** (saque — precisa toggle withdraw) · **C8** IDOR web `orders/eofy/{year}` + `buycomplete/{ObjectId}` (precisa 2ª conta + cookie) · **C6 instant-path** (`/my/sell/now`, requer OK).
