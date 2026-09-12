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
# arapuca.md — CoinSpot · peças pra chain

`protocolo armar a arapuca`. Cada peça é **individualmente fraca** (quase tudo Info). O valor está na **combinação**. Não apagar peça — marcar `[dormant]` se envelhecer sem combinar.

## Peças

| id | peça (1 linha) | acesso p/ usar | sev isolada | o que HABILITA |
|----|----------------|----------------|-------------|-----------------|
| **P1** ~~[MORTO]~~ | Gate de formato do `nonce` = `parseInt(x,10)` + falsy-guard. Passam: `1e30`, `"1e9"`, `"1e"`, `"5."`, `"01"`, `"1n"`, `-1`, `1.5`, `[1]`, `"\t2"`, `"2 3"`. Falham: `0`, `-0`, `".5"`, `"Infinity"`, `"NaN"`, `"  "`, `"００１"`. | unauth | Info | replay / exhaustion do anti-replay **se** o compare/store do nonce usar outro parser (`Number`/raw JSON) → P5 diferencial |
| **P2** ~~[MORTO p/ smuggle]~~ | Parser de body JSON = **LAST-KEY-WINS** em chave duplicada (confirmado unauth: `{"nonce":1,"nonce":"abc"}`→falha; `{"nonce":"abc","nonce":1}`→passa). JSON estrito (lixo após `}` → 400; JSON5 → 400). | unauth | Info | **dup-key smuggling** de `amount`/`address`/`cointype`/`id` **se** o `sign` ou um WAF/validador vê a 1ª ocorrência e o handler a última |
| **P3** | Body só é parseado com `Content-Type: application/json` (match case-insensitive, tolera `;charset`/espaço) **ou** `x-www-form-urlencoded`. `text/plain`/`xml`/`multipart` → não parseado. **Dois encodings aceitos.** | unauth | Info | discrepância de canonicalização do `sign` (HMAC "of POST data") — assina os bytes crus ou params canônicos? re-encodar request assinado |
| **P13** ~~[REFUTADO — ver P20]~~ | **`sign` = HMAC-SHA512 sobre a string JSON CRUA** (`json.dumps(postdata, separators=(",",":"))` — compacta, **não-ordenada, não-canonicalizada**), confirmado nos wrappers de referência. Servidor verifica contra os **bytes recebidos**. | doc/OSINT | Info | **habilita C1**: body hand-craftado com chave duplicada tem `sign` válido (servidor faz HMAC do que recebeu). Só falta um 2º parse divergente (validador vs handler) |
| **P14** | **`nonce` = `int(time()*1_000_000)`** (timestamp µs, ~16 dígitos) — **não é contador**. Legit client depende do relógio; wrappers podem +1 em colisão. | doc/OSINT | Info | **race** (P6): "> último" + "gravar último" atômico? 2 req paralelas mesmo µs. **Cross-versão** (C2): assinatura crua portável v1↔v2 se contadores separados. **Exhaustion** (P1): nonce gigante aceito 1× → relógio real nunca alcança → DoS da key |
| **P4** | Método **não** enforçado na camada de auth: `GET` com body JSON parseia o `nonce`; `DELETE`/`HEAD`/`OPTIONS` chegam na auth. Só `PUT`/`PATCH` barrados (e é a **Cloudflare**, não o app). | unauth | Info | smuggle de request autenticada via `GET`+body passando por middleware/WAF que só inspeciona `POST` |
| **P5** | Ordem da pipeline (por diferencial de erro): `nonce-formato` → `key-presente` (`"no key"`) → `key-existe` (`"invalid key/secret"`) → `sign` → escopo → `nonce-monotônico` → handler. | unauth | Info | cada estágio pré-segredo é fuzzável sem conta; o monotônico é **pós-key** (testar com 1 key) |
| **P6** | V1 API **viva** (`/api`, `/pubapi`, `/api/ro`), mesma middleware ("no key" idêntico). `/api/my/coin/deposit` v1 = **[DEPRECATED] mas documentada/roteável**. | doc + unauth | Info | replay **cross-versão** se o contador de nonce é separado v1/v2 e a assinatura é portável (mesmo "HMAC-SHA512 of POST data") |
| **P7** | Saque API: `emailconfirm` default **NO**; **nenhum param de 2FA** na doc. Termos de Uso dizem "2FA obrigatório para TODO saque". Contradição. | doc | Info→Med? | se a API realmente dispensa 2FA (só a flag da key) → **key vazada = saque sem nenhum OOB**. Casa com hack de 2023 ("private key leak" + "no real-time detection") |
| **P8** | `/withdrawalfiat/confirmed` — **página de display COSMÉTICA** (confirmado: só `?processed=true`/`?expired=true` mudam a mensagem; `?token/t/id/hash/code/confirm/w` = ignorados, len idêntico). O handler real de confirmação (com token) é **outra URL** — achável só com um saque fiat real. | unauth | — (rebaixado) | verificar handler real no **nível 1** (fazer saque fiat, dissecar o link do e-mail) |
| **P9** | `/quote/{buy,sell,swap}/now` retorna **só `rate`** — sem id, sem token, sem expiry. Mas `/my/{buy,sell,swap}/now` aceita `rate` **opcional do cliente** + `threshold` (0–1000) + `direction`. | doc | Info | a UI não tem campo de preço no instant-buy; a API tem. `rate` do cliente é teto protetor ou preço de execução? → P4 tampering |
| **P10** | `/my/buy/edit` exige `cointype, id, rate, newrate` — cliente informa o `rate` **atual**. `newrate` sem bound documentado. | doc | Info | se o servidor localiza/valida a ordem pelo `rate` informado e não só pelo `id` → edita ordem errada. `newrate`→~0 numa ordem de compra |
| **P11** | `/accountrecovery/{email,mobile,2fa,unlockaccount,enablewithdrawals,geolockdisable}` — **todos 200 unauth** (entrada da cadeia de recuperação é pública). | unauth | Info | superfície de ATO/lógica; step-skip / token fraco / fail-open na cadeia (nível 0–1, **sem brutar**) |
| **P12** | `brhash.min.js` = fingerprint de browser completo (canvas, fontes, murmurhash3) enviado ao servidor. | unauth | Info | se algum gate de risco (novo device, geolock, step-up de saque) confia no fingerprint **enviado pelo cliente** → replay do fingerprint da vítima pula 2FA/step-up |

| **P15** | **`/register` usa ALTCHA** (PoW self-hosted). Challenge em `GET /captcha/challenge` (fresco a cada fetch, `salt` com `?expires=` ~5–10min, `signature` HMAC-SHA512). **`maxnumber: 1000000`** → PoW resolvível em sub-segundo. | unauth | Low/Info | **chain-enabler**: anti-automação de `/register` (e talvez login/recovery se compartilham) é negligenciável → viabiliza enum/brute que hoje é "OOS por rate-limit". Bypass total do verify (signature não checada? solução single-use? expires?) = **nível 1** (precisa POST de registro) |
| **P16** | **`/forgotpassword` existe** (200); `/forgottenpassword` e `/passwordreset` → 404. `/join/{code}` → 302 → `/register?code={CODE}` (code **encodado + UPPERCASE**, path fixo — **sem CRLF, sem open redirect**, branch fechado). | unauth | — | `/forgotpassword` = superfície de user-enum / reset-poisoning (nível 0–1, sem brutar) |

| **P17** | **`/ua` e `/ua/event`** — endpoints **unauth, sem CSRF, sem throttle** (POST). `/ua` exige `title,path,referrer,userAgentOverride,fp,language,...` (faltando campo → **500**); `/ua/event` aceita `category,action,label,value` (200 até vazio). Body cap ~poucos KB (20k → 400). Sem reflexão na resposta (é beacon). Escrita **100% atacante-controlada** no store de analytics/atividade interno. | unauth | Med (se blind-XSS) | **blind/stored XSS no dashboard interno** de staff (`path`/`referrer`/`userAgentOverride`/`label` renderizados sem escape); **pré-seed/spoof de `fp`** (P12 concreto — o fingerprint entra aqui sem auth); `userAgentOverride` = UA que o servidor registra (evasão de regra de fraude/geo se lê isso) |
| **P18** | Cookie **`csutm`** = `Base64(JSON.stringify([{c,m,s,t}]))` de `?utm_*` sem sanitizar, `JSON.parse(Base64.decode())` na leitura (**client-side only** — servidor **não reflete** o nome da campanha em `/`, `/register`, `/affiliate`, nem via `?utm_campaign=`). `csua` = `Base64(JSON(Date.now()))` previsível. | unauth | Info (rebaixado) | só vale se um script de página específica usar `val.c`/`val.s` num sink DOM, ou se um dashboard interno renderizar. Baixa prob. |
| **P20** | **Assinatura = HMAC-SHA512 do `canonical(parse(body))`**, NÃO dos bytes crus. Parse ÚNICO (last-key-wins, resolve `\uXXXX` em chave, colapsa dup antes do check de sig), re-serialização compacta preservando ordem, e o **handler usa o mesmo objeto parseado**. Número `1e15`/`.9` → forma diferente → `failed signature`. | key | Info | **mata C1 e toda a classe "sign A / send B" / smuggle por CT/método.** Só sobra divergência se o canonicalizador ≠ código do handler em precisão numérica (H-I) |
| **P21** | **`nonce` = contador monotônico GLOBAL por API key.** Compartilhado V1↔V2, entre bases (`/api/v2`,`/api/v2/ro`,`/api`,`/api/ro`) e **todos** os endpoints. Input `parseInt`-leniente (`"Tabc"`,`" T "`,`"000T"` passam) mas **store + compare são numéricos**. `-1`→`invalid nonce`; `[T]`→`400 invalid data`. | key | Info | replay cross-versão/cross-endpoint MORTO. Só sobra a **atomicidade check-then-store** → race (P14 + P21 → H-A / C5) |
| **P22** | **Escopo da key checado ANTES do dispatch**, V1 e V2. Pipeline: `nonce-fmt → key-present → key-exists → sign → ESCOPO → nonce-mono → handler`. `401 "key type unauthorised"` / `401 "coin withdrawals have not been enabled for this API key"`. | key | Info | nonce-monotônico é DEPOIS do escopo → um 401-escopo provavelmente **não consome nonce** (untested) → amplifica a janela de race (H-A) |
| **P23** | **`/my/coin/deposit` MUTA estado.** 1ª chamada `{cointype:BTC}` → só endereço nativo. Após 1 chamada `{cointype:USDT}` ou `{...,network:ETH}` → **toda** chamada seguinte (qualquer coin, até nunca-consultada) inclui `name:"BNB Smart Chain", network:"ETH", address:<EVM da conta>`. Persiste. Mislabel (BSC rotulado ETH). `network` do cliente não reflete (marker ignorado). | key full | Info→Med? | "read" com side-effect; se UI/cliente chaveia por `network:"ETH"` → user deposita BTC na chain errada = **false top-up / perda** (rubric High). Feeder da C6. Precisa observar crédito de depósito |
| **P24** | **Key V2 autentica na V1** (`/api/ro/*` aceitou key V2 RO); **assinatura portável V1↔V2**. Só o contador único (P21) barra o replay. | key | Info | V1 = superfície viva com **menos params/guardas** (P19) + mesma auth → feeder C7 (order logic bypass via V1) |
| **P19** | **V1 API tem menos guardas** que a V2: `/api/my/buy` = só `cointype,amount,rate` (V2 add `markettype`/`threshold`/`direction`); `/quote/buy` sem `amounttype`; precisão rate 6 casas (V2 difere). Path-suffix + esquema de assinatura **idênticos** à V2. Sem endpoints de saque na V1. | doc | Info | **reforça C2**: body assinado no `/api/v2/my/buy` é byte-idêntico ao esperado por `/api/my/buy` → replay direto se contadores de nonce separados. E colocar ordem na V1 com combos que a V2 rejeita |

> **Retratado:** "reflexão de `?code=` no `/register`" — era `grep` casando "pa**ssword**". `?code=` **não reflete**. Não é peça.
> **Fechado (unauth):** ALTCHA `?maxnumber=` — cliente **não** controla dificuldade (server-fixed 1M). `/my/*` e `/api/v2/*` — **nenhuma rota pula a middleware** (sweep: tudo 302/401). `coinspot.min.js` = jQuery+jqPlot+qrcode vendored, **zero código de app**. `csutm` — sem reflexão server-side. **Cache poisoning** — nada dinâmico é cacheável (`no-store`+`DYNAMIC`); só JS estático (sem input). **Subdomínios** — 0 de ~90 nomes comuns resolvem (footprint = `www` + apex + 3 hosts de e-mail). `/withdrawalfiat/confirmed` = cosmética. APK — apkpure/apkmirror/apkcombo 403/404; sem `jadx`/`apktool` local (→ você baixa ou instalo tooling).
> **Fora de escopo (não tocar):** `coinspot.com`→3.33.251.168, `coinspot.co`→3.33.130.190 (AWS Global Accelerator — prováveis redirects defensivos da CoinSpot); `coinspot.io`→5.188.177.252 (parking, provável squatter). H1 = só `coinspot.com.au` closed scope.

## Chains candidatas (passo de combinação)

> 🚫 **C1 REFUTADA (2026-09-09, mecânica) — ver abaixo.** O server **não** assina bytes crus:
> ele faz **um** `parse` do JSON (last-key-wins, resolve `\uXXXX` em chaves), **re-serializa
> compacto** e HMACa esse **canônico**; o handler usa o **mesmo objeto parseado**. Provado com
> probes assino-A-envio-B em `/api/v2/ro/status` (H1/H4/K1/K3/K4). Dup-key colapsa pro último
> valor **antes** do check de assinatura → o atacante é obrigado a assinar a forma já colapsada
> = exatamente o que o handler executa. **Sem 2º parse, sem divergência.** P2+P13 mortos p/ este fim.
> C2 também morta (contador de nonce global por key). Lead B (parser do nonce) negativo.

### ⭐ C1 — Dup-key smuggling no saque  =  P2 + P13 + P5   ~~[MORTA]~~
`{"nonce":N,"amount":"0.00001", ... ,"amount":"9999","address":"MEU", ... ,"address":"ATACANTE"}`
- **P13 resolve a dúvida:** o `sign` é HMAC dos **bytes crus** → hand-crafto o body com chave duplicada, calculo `sign = HMAC(secret, esse_string_exato)`, o servidor faz HMAC do que **recebeu** → **assinatura casa**. Um client normal (dict/obj) não consegue gerar chave duplicada; eu sim, montando o raw body.
- **P2 confirmado unauth:** o parser do servidor é **last-key-wins** e não rejeita chave duplicada.
- **O elo que falta:** existe um **2º parse divergente**? (validador de schema/limite lê a 1ª ocorrência OU rejeita duplicada, enquanto o handler de saque lê a última). Em Express típico com `express.json()` é um parse só (sem diferencial). Vale se houver: middleware de verify sobre raw-body + validador com lib diferente + handler.
- **Impacto se real:** saque de valor/endereço diferente do assinado/validado = **Critical**.
- **Teste (nível 1, 1 key full+withdraw):** `senddetails` normal → `send` com body hand-craftado, `amount` duplicado (`0.00001` depois um valor **acima do saldo** — falha segura), ver se a mensagem de erro reflete o 1º ou o 2º valor. **Para-e-confirma antes de qualquer `send` com valor real.**

### C2 — Nonce replay cross-versão  =  P1 + P5 + P6
Assinar `/my/buy` uma vez → enviar pra V2 (nonce N aceito) → reenviar os **mesmos bytes** pra V1 (`/api/my/buy`, contador da V1 em 0) → execução dupla.
- **Elo faltando:** 1 API key; confirmar (a) contadores de nonce separados v1/v2, (b) assinatura portável entre versões, (c) V1 aceita a key V2.

### C3 — [REBAIXADA pela policy] Design de saque sem 2FA  =  P7 (+ P12)
⚠️ "Reports of publicly leaked account credentials" = **OOS**. Então "key vazada no GitHub" **não vale por si**.
O que sobra: se a API **realmente** dispensa 2FA no saque (só a flag da key) — contradizendo os Termos ("2FA p/ todo saque") — isso é uma **fraqueza de design nova** demonstrável **com a tua própria key** (não precisa de key vazada). Impacto: qualquer comprometimento de key = drain sem OOB. Enquadrar como weakness do "underlying technology".
- **Elo faltando:** confirmar no nível 1 que `send`/`send/async` não exigem nenhum fator além da flag. P12 (spoof de fingerprint) se houver step-up por device.

### C4 — Captcha-off → user-enum / brute  =  P15 + P16 (+ P11)
`maxnumber:1000000` = automação de `/register` / `/forgotpassword` é barata. Se o verify do ALTCHA não faz single-use ou não checa `signature` → captcha 100% bypassável → enum de e-mail + brute de reset/recovery viram viáveis (hoje "OOS por rate-limit").
- **Elo faltando:** 1 POST de registro pra ver o contrato do verify (nível 1); confirmar se login/recovery compartilham o ALTCHA.

**Melhor chain agora:** **C1** (dup-key smuggling no saque). Precisa: 1 key full+withdraw. Shape de Critical.
`code search` por keys vazadas (C3): GitHub code search agora exige login (WebFetch não passa); grep.app 429. **Pendente** — tentar via `gh` se instalado, ou wrappers conhecidos (`fatton139/coinspot-api`, `geekpete/py-coinspot-api`, `samuraitruong/coinspot-api`, `zahav/cryptobot`) — checar commits/CI/.env desses e forks.

---

# Modelo do sistema — `protocolo descobrir a roda` (2026-09-09)

Reconstrução a partir de tudo que sangrou no nível 1 (key RO + key full/trade).

## Como a API v2 realmente funciona (confirmado nesta sessão)

1. **Assinatura ≠ bytes crus.** O server faz **UM** `parse` do JSON do body, **re-serializa
   canônico** (compacto, sem espaço, preservando ordem do parse, resolvendo `\uXXXX` em
   chaves, colapsando dup-key **last-wins**, normalizando número: `1e15`/`.9` → forma
   diferente → `failed signature`) e HMACa **esse canônico**. O handler usa o **mesmo objeto
   parseado**. ⇒ Zero divergência validador-vs-handler. Mata C1, mata "sign A / send B",
   mata smuggle por content-type/método.
2. **Nonce = contador monotônico GLOBAL por API key.** Compartilhado V1↔V2, entre bases
   (`/api/v2`, `/api/v2/ro`, `/api`, `/api/ro`), entre **todos** os endpoints. Input
   `parseInt`-leniente (`"Tabc"`, `" T "`, `"000T"` passam) mas **store e compare são
   numéricos** → sem replay por lixo, sem DoS lexicográfico.
3. **Escopo da key (RO / full / withdraw) checado ANTES do dispatch**, em V1 e V2.
   `401 "key type unauthorised for this request"` / `401 "coin withdrawals have not been
   enabled for this API key"`. Pipeline: `nonce-fmt → key-present → key-exists → sign →
   ESCOPO → nonce-monotônico → handler`.
4. **Key V2 autentica na V1** (`/api/ro/*` aceitou key V2 RO). Assinatura portável V1↔V2.
   Contador de nonce único é o backstop do replay cross-versão.
5. **`/my/coin/deposit` MUTA estado da conta.** 1ª chamada `{cointype:BTC}` → só endereço
   nativo. Após 1 chamada com `{cointype:USDT}` ou `{...,network:ETH}` → **toda** chamada
   seguinte (qualquer coin, até nunca-consultada) passa a incluir `name:"BNB Smart Chain",
   network:"ETH", address:<EVM da conta>`. Persiste. Endpoint "read" com side-effect.
6. **Quotes bem validados** (`amount` ≤0 → 400, `amounttype` inválido → 400).

## Fronteiras de confiança e a suposição de cada uma

| # | Fronteira | Suposição do dev | Costura a atacar |
|---|-----------|------------------|------------------|
| B1 | unauth ↔ `/pubapi` | só dado público sai | — (fechado) |
| B2 | posse da key ↔ HMAC | quem assina = dono da key | canonicalização vs handler em precisão numérica de campo `$` |
| B3 | escopo RO/full/withdraw | check pré-dispatch cobre **todo** endpoint/versão | endpoint não-documentado que pula o check? `/my/coin/deposit` (side-effect) roda com RO? |
| B4 | nonce ↔ replay | check-e-store é atômico | **race**: N requests paralelas, mesmo nonce > último, passam o compare antes de qualquer store |
| B5 | key ↔ 1 conta | key só toca dados da própria conta | (precisa 2ª conta) |
| B6 | sessão web ↔ `/my/*` | cookie = dono; `{ObjectId}`/`{year}` não enumerável | `/my/orders/eofy/{year}`, `/my/buycomplete/{ObjectId}` — ownership? (precisa cookie) |
| B7 | deposit crediting | endereço X na chain Y credita coin Z certo | mislabel `BNB Smart Chain`→`network:"ETH"`; provisioning por chamada |
| B8 | order matching | `rate` do cliente é **teto protetor**, não preço de execução | `/my/buy/now` + `rate` stale + `amounttype:coin`; `threshold` 0–1000 e `direction` sem semântica documentada |
| B9 | V1 handler | V1 = subconjunto seguro da V2 | V1 `/api/my/buy` sem `threshold`/`markettype` → pula guarda que só existe na V2? |
| B10 | withdrawal ↔ saída de $ | flag da key + 2FA barram | `emailconfirm` default NO; `network`/`paymentid` confusion; Termos dizem "2FA sempre" (contradição) — **precisa toggle withdraw** |

## Hipóteses novas `[UNTESTED]` — rankeadas

| id | hipótese | fronteira | probe | precisa | EV |
|----|----------|-----------|-------|---------|----|
| **H-A** | **Race no nonce** — check-then-store não-atômico → N requests paralelas com mesmo nonce (>último) todas passam → primitiva de replay/double-execution (double withdraw/order/redeem) | B4 | 🟢 **detecção segura**: burst de ~30 requests idênticas (mesmo nonce+sign+body) a `/api/v2/ro/status` em paralelo → contar quantas dão 200. >1 = gate racy | key (tenho) | **ALTO** |
| **H-B** | `/my/buy/now`/`/my/sell/now`: `rate` do cliente é preço de **execução**, não teto → comprar caro pagando `rate` mínimo / vender barato recebendo `rate` gigante contra a plataforma. `amounttype:coin` + `rate` fora do mercado | B8 | 🔴 1 ordem `/my/sell` mínima (0.00001 BTC) a `rate` 2× mercado (não preenche) → inspeciona campos → cancela. para-e-confirma | key full (tenho) | **ALTO** |
| **H-C** | `threshold` (0–1000) e `direction` em `buy/now`/`sell/now` sem semântica documentada — se `threshold` é slippage tolerado e `direction` inverte lógica de gatilho → ordem executa do lado errado / com slippage absurdo | B8 | 🔴 mesma ordem-sonda de H-B com `threshold`/`direction` variados | key full | MÉDIO-ALTO |
| **H-D** | V1 `/api/my/buy` (só `cointype,amount,rate`) pula uma guarda `threshold`/`markettype` que só a V2 aplica → ordem na V1 com combo que a V2 rejeita | B9 | 🔴 1 ordem-sonda via V1 vs V2, mesmo payload-base | key full | MÉDIO |
| **H-E** | `/my/sell/edit` (`cointype,id,rate,newrate`) localiza a ordem por `rate` (não só `id`) → `id` da ordem X + `rate` da ordem Y edita a errada; `newrate`→0/negativo | B8 | 🔴 2 ordens-sonda minhas a rates diferentes → edit cruzando id/rate → cancela | key full + 2 ordens | MÉDIO |
| **H-F** | Web `/my/orders/eofy/{year}` e `/my/buycomplete/{ObjectId}` (cookie auth, path diferente da API) não checam dono → IDOR de relatório fiscal / confirmação de compra (§7 paridade browser-vs-API) | B6 | 🔴 precisa cookie de sessão (`code -1` com cURL do DevTools) → trocar `{year}`/`{ObjectId}` | cookie web | MÉDIO |
| **H-G** | `withdraw/send` com `network` divergente do `cointype` (ex. BTC + `network:ETH` + address `0x…`) → debita BTC e envia on-chain errado / não envia; ou `paymentid` injection em coin de memo (XRP/XLM) | B10 | 🔴 erro-diferencial (amount > saldo / address inválido) — sem send real | **toggle withdraw** | ALTO (se real) |
| **H-H** | `withdraw/send/status` `pollid` (GUID) sem checagem de dono → ler destino+valor de saque alheio | B10 | 🔴 gerar 1 pollid próprio → fuzzar formato / procurar vazamento | toggle withdraw | BAIXO-MÉDIO |
| **H-I** | Canonicalização (sig) vs handler em **precisão numérica** de campo de `$`: `{"amount":1e-8}` quebra sig, mas `{"amount":"0.00000001"}` passa — se handler faz `float()` e matching faz `Decimal()` sobre a mesma string → discrepância de arredondamento acumulável | B2 | 🔴 ordens-sonda com `amount` em formas limítrofes | key full | BAIXO |

**Ordem sugerida:** **H-A** (grátis, seguro, alto EV — fazer já) → **H-B/H-C** (1 ordem-sonda de venda, para-e-confirma) → **H-D** (V1) → **H-G** quando o toggle de withdraw entrar → **H-F** se o Tiago colar o cookie web.


---

# Chains novas — `armar a arapuca` (2026-09-09, pós-descobrir-a-roda)

### ⭐ C5 — Race no nonce → N× execução de 1 request assinado  =  P14 + P21 + P22
`nonce` é contador global por key (P21); o gap é a **atomicidade check-then-store** (P14).
N requests paralelas com `nonce = último+1` (todas > último) passam o compare **antes** de
qualquer store → todas chegam ao handler → **1 saque/ordem/redeem assinado executa N vezes**.
- **Elo faltando:** confirmar o gate racy. **Probe seguro (🟢):** burst de ~30 requests
  idênticas (mesmo nonce+sign+body) a `/api/v2/ro/status` em paralelo → contar 200s. >1 = racy.
- **Impacto se real + key withdraw:** double/N-withdraw = **Critical** (funds outside user context).
- **Substitui a C1 como chain-alvo #1.** Não precisa de nada novo pra provar o gate.

### ⭐ C6 — `rate` do cliente = preço de execução (não teto)  =  H-B + H-C  (+ P23 p/ narrativa "false credit")
`/my/{buy,sell}/now` aceita `rate` **opcional** do cliente + `threshold` (0–1000) + `direction`
sem semântica documentada. Se `rate` é o preço a que a ordem executa (não um limite protetor):
vender 0.00001 BTC informando `rate` = 100× mercado → **AUD criado do nada contra a plataforma**.
- **Elo faltando:** 1 ordem-sonda de venda mínima a `rate` absurdo (não preenche) → ler os
  campos de retorno (executa? a que preço? `threshold`/`direction` mudam?) → cancelar.
  **para-e-confirma.** (Tiago já autorizou ordens-sonda.)
- **Impacto se real:** manipulação de saldo do próprio user = **High**; se sangra o book/tesouraria = **Critical**.

### C7 — Order-logic bypass via V1  =  P24 + P19 (+ H-D)
V1 `/api/my/buy` = só `cointype,amount,rate` (sem `threshold`/`markettype`/`direction` da V2).
Mesma auth (P24). Se a validação de `threshold`/mercado vive só no path da V2 → ordem na V1
com combo que a V2 rejeita (ex. `rate` fora de banda sem `threshold` pra barrar).
- **Elo faltando:** ordem-sonda idêntica V1 vs V2 (após C6 caracterizar o normal).

### C8 — IDOR web fiscal/compra (paridade browser-vs-API §7)  =  H-F + recon web nível 0
`/my/orders/eofy/{year}` (path por ano, trivial) e `/my/buycomplete/{ObjectId}` (ObjectId
semi-previsível — timestamp+contador) usam **cookie de sessão** (path diferente da API HMAC).
Se o handler não checa dono → relatório fiscal / confirmação de compra de outra conta.
- **Elo faltando:** cookie de sessão web (`code -1` com "Copy as cURL") + (p/ impacto pleno) 2ª conta.

### C9 — Deposit chain-confusion → false top-up  =  P23
Mislabel `BNB Smart Chain`→`network:"ETH"` é dado server-side, platform-wide. Se um user
lê `network:"ETH"` e deposita numa opção que é BSC (ou vice-versa) → crédito errado / perda.
- **Elo faltando:** observar o comportamento de **crédito de depósito real** (fora do acesso
  seguro atual). Provável by-design (suporte Binance-Peg) — **manter como `[dormant]` até ter evidência de crédito.**

**Melhor chain agora:** **C5** (race no nonce) — probe 🟢 grátis, não precisa de nada novo, shape
Critical. Depois **C6** (rate = execução) com 1 ordem-sonda de venda. C1/C2 mortas.
