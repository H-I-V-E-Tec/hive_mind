---
program_id: yahoo
document_type: note
claimed_scope_status: unknown
classification: internal
source: intigriti policy (yahoo) — 2026-09-08
collected_at: 2026-09-12T00:00:00Z
tags: [root, yahoo]
asset_refs: [apis.mail.yahoo.com]
---
# Yahoo (Paranoids) — ficha de alvo

- **Programa:** Yahoo Bug Bounty ("Paranoids") · **Intigriti** (público) · media/ads global
- **Link:** intigriti.com/programs (Yahoo) · **Reward:** Tier 2 — Low $100–500 · Med $500–3k · High $3k–10k · Crit $10–12.5k · **Except. $12.5–15k**
- **Prioridade:** 🟡 **Média-Alta (fit Alta) ⭐** — amplitude máxima + fit BAC/IDOR/authz + SSRF que paga + money (Fantasy Wallet). Saturação de década = único freio.

## ⛓️ Constraints pré-fixadas (SEMPRE aplicar)
1. **Contas:** ✅ **conta de teste ativa = `moldret_intigriti@yahoo.com`** (autorizada pelo Tiago 2026-09-07 p/ login em qualquer área Yahoo). ⚠️ o programa **prefere** `<username>+x@intigriti.me` p/ elegibilidade em algumas properties — confirmar se algum achado exige esse formato. Self-register na maioria dos apps.
2. **Rate:** **máx 50 req/s**.
3. **Header obrigatório em TODO tráfego ativo:** `X-Bug-Bounty: Intigriti-<username>` (ou `ID-<sha256>` / `<toolname>`). ✅ username = `moldret` → header `X-Bug-Bounty: Intigriti-moldret`.
4. **IP no report** (eles cruzam com logs).
5. **Anexos SÓ no report** — nunca YouTube/Vimeo/S3/externo, nem PoC hospedado fora.
6. **Não pivotar / não exfiltrar** — PoC mínima; dado sensível só p/ validar, depois devolver.
7. **Ceasefire:** se postarem, blackout de ~90d por classe — parar naquela classe.
8. **Disclosure só com permissão escrita.**

## 🎯 Escopo
- **Wildcards (por token, não `*.yahoo.com` cheio):** `*ensemble*.yahoo.com` · `*omega*.yahoo.com` (o sub precisa **conter** o token). → recon de subdomínio (Nível 2/3).
- **URLs exatas:** `apis.mail.yahoo.com` · `data.mail.yahoo.com` · `onepush.query.yahoo.com` · `proddata.xobni.yahoo.com` · `yimg.com`.
- **Produtos (Other, Tier 2):** Mail · Finance · Search · News · Calendar · Weather · Video · **Sports: Fantasy / Fantasy Wallet / Daily Fantasy / PicknWin / Best Ball / Editorial / Mobile** · eCommerce TW (Auctions/Shopping/Used Car) · TW Media (Front Page/News/Stock) · Yahoo HK News · Gemini · Identity Services · Low Cost Access · 7 News · Social Media Accounts · Open Source Projects · Yahoo! (Misc).
- **Política:** achou vuln em asset Yahoo não-listado → reportar mesmo assim (pode valer).

## 🚫 Fora de escopo
`*.yahoo.net` · `*.yahoo.com.tw` · `*.yahoo.com.hk` · `*.yahooinc.com` · `*.vdms.com`/`*.verizondigitalmedia.com` · SSP/ad platforms (ssp.verizonmedia.com, adaptv.advertising.com, alephd.com, vidible.tv, store.vzbuilders.com…) · produtos vendidos/EOL (Tumblr, Flickr, AOL, Engadget, TechCrunch, MapQuest, Yahoo Answers/Groups/Messenger, Yahoo Japan…) · Cricket, Factual, Wagr, CommonStock, Artifact.news, Vetted.ai · `files.molo.ch`.
⚠️ **Nuance:** "TW eCommerce" é asset in-scope, MAS `*.yahoo.com.tw` é OOS — confirmar o domínio real do produto antes de tocar.
**Borderline (sem bounty):** self-XSS, clickjacking, missing headers, open redirect intencional, CSRF de login/logout, results de scanner, enumeração de conta, version disclosure.

## 🍌 SSRF testbed "bananastand" (CWE-918 — eles DÃO os alvos e pagam)
Pra provar SSRF, ler/escrever arquivo num destes (Prod **e** Corp), anexar arquivo + timestamp + host:
- **Prod:** `banana.stand.{ne1,gq1,bf1,bf2,sg3,ir2,tw1,tp2}.prod.oath` e `banana.stand.{ne1,gq1,bf1,bf2,sg3,ir2,tw1,tp2}.yahoo.com`
- **Corp:** `banana.stand.corp.{gq1,bf1,sg3,ne1}.cic.oath` e `banana.stand.{cgq1,cbf1,csg3,cne1}.yahoo.com`
- Arquivos: `<ext>_###.<ext>` (ex. `txt_001.txt`, `zip_001.zip`) + `noext_01`; 404 = "no bananas for you!". Dica deles: também buscar `http://<host>/intigriti-<username>` pra marcar nos logs.
- ⚠️ **SSRF que NÃO toca o bananastand → risco de $0.** Todo param de fetch de URL (image proxy, `onepush`, webhooks, importadores) é candidato.

## ⚰️ Veredito L7 (2026-09-07): routing-SSRF in-band MORTO — ATS `remap_required` Yahoo-wide
Testado via socket cru contra 6 edges: Host não-allowlistado → `404 Not Found on Accelerator` (não roteia); absolute-URI/@/porta → `400 Invalid`. Bananastand in-band inalcançável. Resíduo só via Collaborator OOB (manual). **Lead #0/CHAIN-A rebaixados.** Novo top lead = **L11 Lightyear CMS** (abaixo).

## 🆕 L11 — Lightyear CMS / Yahoo Creators (`cm-ui.yahoo.com`) ⭐⭐ NOVO TOP LEAD (fit BAC/IDOR #1)
Next.js 200 unauth (sem CSP). Backend vazado no bundle: `cm-auth-service`(FastAPI) · `content-service`(AWS API GW) · `api.creators`(Spring OAuth2) · `api.yahoo.com`(interno). Auth = `Bearer` JWT do `localStorage.access_token`; **`activeTeam` vem do localStorage (cliente controla)** → BOLA/cross-tenant em `/assignments/me/team/{id}`, `/admin/user/{id}`, `/earnings`. **Bloqueio:** access_token de conta CMS/creator (Tiago onboarda em Creators → DevTools localStorage). Detalhe em `a-cacada-ate-aqui.md` + `arapuca.md` (Y22-Y27, CHAIN-E).

### 🔑 Correção de auth (OSINT Wayback 2026-09-08) — o login do CMS é **Auth0**, não Yahoo-IdP/Spring
CDX de `cm-auth-service.yahoo.com` (237 URLs `/login?referrer_url=...` arquivadas) revela: **IdP = Auth0 tenant `cm-production-yahooinc.us.auth0.com`**; `cm-auth-service` expõe **`/login` `/logout` `/token`**; fluxo `/login?referrer_url=<cm-ui c/ ?code&state_token>&app_id=lightyear`→307 (codes arquivados expirados). Onboarding público confirmado em `creators.yahoo.com/sign-in` (self-serve).
- **⚠️ Auth0 tenant é `*.auth0.com`+`yahooinc` = terceiro/OOS** (`*.yahooinc.com` fora de escopo) — não tocar; só intel. Client_id `714H5NHjrv29HLgBBCrBdsdLX58P4m3G` é público por design (não é leak).

### ⭐ CHAIN-J [PARCIALMENTE CONFIRMADA · unauth] — code-leak OAuth → ATO de conta Creator (probe ativo Y-OSINT-1, 2026-09-08)
Testado ativo (header `X-Bug-Bounty: Intigriti-moldret`, serial, não-destrutivo). Detalhe/peças Y29–Y33 em `arapuca.md`.
- ✅ **Confirmado:** `referrer_url` do `cm-auth-service/login` aceita **qualquer `https://*.yahoo.com`** (não pina em cm-ui); o `/callback` **reflete `code`+`state_token` do Auth0 na URL do `referrer_url` sem validar o code** (`307 R?code=…`); `/authorize` **sem PKCE**.
- ❓ **Falta (chain-required — §4/§11: 307 ≠ impacto):** (L1) um host `*.yahoo.com` que open-redirecione/vaze a URL = o "ralo" (chain-search §12, ativo, executável); (L2) trocabilidade do code no `/token` (GET-only, params cookie-bound → leg autenticada); demonstrar o code real end-to-end (token do Tiago).
- **Próximo elo:** caçar o ralo L1 (open-redirect/url-leak em `*.yahoo.com`) → fecha até ATO sem conta.

### 🏈 Fantasy Wallet / API v2 — read-IDOR testado a fundo, NEGATIVO (2026-09-08, sessão real via `code -1`)
Conta `moldret_intigriti` autentica no Fantasy sem gate de aprovação (diferente do CMS). Mapa: `pub-api-ro.fantasysports.yahoo.com/fantasy/v2/league/<key>/{settings,scoreboard,standings,transactions,teams,draftresults}` (gramática YQL, resource por key `nfl.l.<id>`/`nfl.t.<id>`).
- ✅ **Matriz completa testada (§5):** liga privada alheia → **403 consistente** em todos os sub-recursos (siblings); verb tampering (OPTIONS/HEAD/PUT) não bypassa; **batch smuggling** (`league_keys=minha,alheia` e ordem invertida) → **403 do lote inteiro** (fail-closed). Ligas que responderam 200 são `league_type: public` + `is_cash_league: 0` (template Yahoo, público por design — FP, não IDOR).
- **Veredito: NEGATIVO sólido**, não "não achei o param certo" — rodei siblings + verbo + batch antes de encerrar (§3.1).
- **Não testado (limite de escopo):** escrita (roster/add-drop/transação) em time de outro usuário real — exigiria tocar conta de cliente sem autorização, ou uma 2ª conta própria pra provar BOLA sem esse risco.
- **Próximo alvo seguro:** `checkout.fantasysports.yahoo.com` → `/api/v1/checkout/cart/product/bundled` — lógica de preço/qty testável **sem tocar outro usuário** (mutar valores no próprio carrinho).

## 🎯 Leads priorizados [UNTESTED] (fit → memory/arsenal)
0. **[L7 · CDN-proxy] SSRF/host-confusion** — ⚰️ **MORTO in-band (ver Veredito L7 no topo)** — ATS `remap_required` bloqueia; rebaixado. Resíduo só via Collaborator OOB.
1. **[Fantasy Wallet] money/lógica** — hosts: `checkout.fantasysports.yahoo.com` (307/Envoy) + dev/qa/stage, `api.fantasysports.yahoo.com`, `wallet.secure.yahoo.com`. `S-FUND-01`/`S-RACE-01`/business-logic: IDOR/authz em saldo/entry-fee/checkout; race em depósito/saque. **Precisa conta + mapear API autenticado. Fit máximo.**
2. **[Mail/Calendar] IDOR/authz** — `apis.mail.yahoo.com` + **`caldav.calendar.yahoo.com` (401 — CalDAV auth-gated)**: `S-IDOR-01` (id de mensagem/pasta/evento/conta) → ler dado alheio.
3. **[SSRF] bananastand** — qualquer fetch-de-URL nos produtos → CWE-918 estruturado (alvos dados). Baixo atrito, paga.
4. **[Identity/auth] SSO/OAuth + non-prod** — `authnapi.login.yahoo.com` (Google Cloud CDN), `api.authnapi`, envs `beta/gamma/external*.login` (ATS). `S-OAUTH-*` (redirect_uri, cross-property) + reuso prod↔non-prod. Ecoa NBA L4.
5. **[wildcards ensemble/omega]** — hosts internos/serviços frescos (menos lavados) → authz. **Depende do recon Nível 2/3.**
6. **[eCommerce TW]** — marketplace = lógica/IDOR (⚠️ confirmar domínio in-scope vs `.com.tw` OOS).
7. **[L8 · PAGAMENTO — do recon externo] ⭐⭐** — `payments.yahoo.com`, `token-service.payment`, `subscriptions.payments`, `origin.checkout.*` (**AWS ELB = origem direta atrás do CDN → direct-origin/host-confusion + WAF bypass**), `oidc.checkout.*` (**OIDC no checkout = casa o dojo OAuth: `redirect_uri`/dirty-dancing + lógica de pagamento**). Fit MÁXIMO (dinheiro + OAuth + BAC). Quase tudo 403 → precisa conta/fluxo autenticado.
8. **[L9 · OAuth API dedicada]** — `api.oauth.yahoo.com`/`sapi.oauth2.yahoo.com` (HTTP!)/`acctapi`/`umapi`/`partner.login` → `S-OAUTH-REDIR-02`/`S-OAUTH-LEAK-01`/`S-OAUTH-CARRYOVER-01` (os sinais do dojo). SSO cross-property + redirect_uri.
9. **[L10 · Admin panels]** — `mario-admin.nevec`, `admin.nevec`, `hermes-admin.ec`, `hk.admin.deals` (403) → CWE-306/284 (interface admin exposta / authz).

## Recon — `protocolo ladrão de bancos`

### Nível 1 (Passivo) — FEITO 2026-09-07 (zero toque na Yahoo)
- **crt.sh:** 502 (fora do ar). **subfinder** passivo em `yahoo.com` (sem API keys) → **40 hosts in-scope** (todos `omega`/`ensemble`; "omega" = plataforma k8s/edge da Yahoo). ⚠️ **sub-enumerado** — sem crt.sh/keys, cobertura parcial; Nível 3 pega o resto. Cru: `recon/subfinder_yahoo.txt`, `recon/inscope_wildcards.txt`.
- **Padrões quentes achados (categorizados em `recon/hot_*.txt`):**
  - 🔥🔥 **CDN-proxy que embute domínio externo** (`recon/hot_cdn_proxy.txt`, 6 hosts): `antivirus.google.com.amp-timeinc-net.cdn.production.omega.gq1.yahoo.com`, `antivirusupport.gizmodo-com.cdn.researchinpractice.production.omega.gq1.yahoo.com`, `...api-cointelegraph-com.cdn...`, `...lifehacker-com...`, `...www.recode.amp-timeinc-net...`. Padrão = `<origem-externa>.cdn.<...>.production.omega.gq1.yahoo.com` → CDN que **proxeia origens arbitrárias**. **= candidato SSRF / host-confusion / cache-poisoning**, e a Yahoo TEM bounty SSRF + testbed bananastand. **Lead #1 novo (L7).**
  - 🔥 **non-prod / envs fracos** (`recon/hot_nonprod.txt`): `admin.stg-blk01.omega.bf2.yahoo.com` (admin em staging!), `boot-api-{dev,pr,staging}.media-edge-k8s`, `aud-gca.{development,canary,pullrequest,staging}.omega.bf1`, `bjn-canary1-*`, `alpha1/beta1`. Non-prod = authz mais fraca / reuso prod↔dev.
  - 🔥 **API/money** (`recon/hot_api_money.txt`): `boot-api.media-edge-k8s.omega.yahoo.com`, `apple.finance-yql-production.finance-k8s.omega.yahoo.com`, `billing-tw-k8s.omega.yahoo.com`, `auctions-tw-desktop-k8s.omega.yahoo.com`.

### ⚰️ L7 — CDN-proxy `*.cdn.production.omega.gq1.yahoo.com` (SSRF/host-confusion) — MORTO in-band (histórico; ver Veredito L7 no topo)
Hosts que embutem origem externa como label. **Precedente forte:** o "Cracking the Lens" (Kettle) explorou **routing-SSRF na própria Yahoo** (`ats-vm.lorax.bf1.yahoo.com`, Apache Traffic Server) por $20k×2 — mesma família `omega`/`bf1`/`gq1`.
- **Probe A — routing-SSRF (`S-SSRF-ROUTE-01`, 🔴 precisa header):** contra os 6 hosts `hot_cdn_proxy.txt`:
  1. `Host: <id>.<collab>` → callback? (o proxy roteia por Host).
  2. absolute-URI: `GET http://banana.stand.gq1.yahoo.com/txt_001.txt HTTP/1.1` + `Host: <host omega>` → ler o arquivo do **bananastand** = **SSRF provado e PAGO**.
  3. `@`-notation: `GET @<collab>/ HTTP/1.1` + `Host: <host omega>`.
  4. porta/host: `Host: <host omega>:80@<collab>`.
  - Confirmação: callback OOB / latência anômala / banner ATS "Traffic Server Overseer". Escala: `169.254.169.254` metadata → IAM; ou protocolo do ATS (`HELP`/`SET proxy.config.*`).
- **Probe B — cache poisoning (`S-WCP-01`, 🟢 com cache-buster):** `?cb=rand` + `X-Forwarded-Host: canary` nos hosts com `CF-Cache-Status`/`Age`/`X-Served-By` → reflexão de `canary`? reenviar sem header → persistiu = poison. **Nunca sem cache-buster.**
- Casar com `memory/arsenal.md` (SSRF routing-based + Web Cache Poisoning) + `signals.md` (`S-SSRF-ROUTE-01`/`S-WCP-01`).

### Nível 2 (Médio) — FEITO 2026-09-07 (header `X-Bug-Bounty: Intigriti-moldret`, rate baixo)
- **Resolução:** só **2 dos 40** hosts CT resolvem publicamente; o resto do `omega` é **interno/inalcançável de fora** (CT vaza, DNS não). Os 6 hosts L7 (CDN-proxy) e toda a família `omega.gq1` **não resolvem**.
  - `adm-sauroneye-omega.tns.nevec.yahoo.com` → istio-ingressgateway `twec-prod1` (eCommerce TW prod, AWS oath.cloud) — resolve, mas **não serve HTTP de fora**.
  - `admin.stg-blk01.omega.bf2.yahoo.com` → `74.6.101.178` — idem (resolve, HTTP morto de fora).
- **🔥 Achado sistêmico:** **TODO edge Yahoo alcançável = Apache Traffic Server (ATS)** (mail/finance/apis.mail/xobni/yimg), Envoy atrás. **ATS é exatamente o alvo do "Cracking the Lens" na Yahoo** → **L7/routing-SSRF é Yahoo-wide**, não só os hosts omega.
- **WCP (`S-WCP-01`) — negativo-por-ora:** `X-Forwarded-Host: canary` (com cache-buster) em `finance`/`mail`/`s.yimg` → **sem reflexão**. `mail` tem `age:0`/`vary`, `yimg` `no-store`. Falta fuzz de outros headers unkeyed + endpoints não-front-page (passo hunter).
- **Rate:** `finance.yahoo.com` deu **429** (bot-mgmt) rápido → manter volume mínimo.

### L7 reenquadrado (pós-Nível 2)
Os hosts vazados são inalcançáveis, MAS a técnica **não precisa** deles resolver — manda-se o `Host`/absolute-URI forjado a um **edge ATS alcançável** (qualquer produto público) e vê se roteia interno. **É passo de `formação de lança`/hunter, não recon:** requer Collaborator OOB (não tenho) OU usar o **bananastand como prova in-band** (`GET http://banana.stand.gq1.yahoo.com/txt_001.txt HTTP/1.1` via edge). Probe B (WCP) segue como fuzz de header no hunter.

### Nível 3 (Completo) — FEITO 2026-09-07
- **Fontes:** crt.sh (voltou; 1731) + certspotter (686) + subfinder → **2329 subs yahoo.com únicos** (`recon/all_subs.txt`). In-scope wildcard completo: **72** (`inscope_wildcards.txt`). Hosts de produto: **217** (`product_hosts.txt`) → **70 resolvem, 51 vivos** (`n3_probe.jsonl`).
- **🔥 L1 money (hosts reais — `recon/hot_money.txt`):** `checkout.fantasysports.yahoo.com` **307** (Envoy — fluxo de pagamento) + `dev/qa/stage.checkout.fantasysports` · `football.fantasysports.yahoo.com` **200** (produto vivo) · `tournament`/`golf`/`fantasysports` (30x) · `api.fantasysports.yahoo.com` · `beacon.fantasysports` (Express) · `wallet.secure.yahoo.com` (não resolveu — retestar). **→ checkout = lógica/IDOR de dinheiro.**
- **🔥 L4 auth (hosts reais — `recon/hot_auth.txt`):** `authnapi.login.yahoo.com` **403 (Google Cloud CDN!)** + `sapi/alpha/stage.api.authnapi` (500) · **envs non-prod** `beta/gamma/externalbeta/externalgamma.login`, `beta.protect.login`, `beta/gamma.api.login` (ATS "Not Found on Accelerator") · `login.yahoo.com` 200. **→ API de auth rica + non-prod = SSO/OAuth (ecoa NBA L4) + reuso de env.**
- **🔥 Outros:** `caldav.calendar.yahoo.com` **401** (CalDAV auth-gated → IDOR de calendário alheio) · `billing.finance`/`edit.finance` em **AWS CloudFront** (stack ≠ ATS; money) · `mail.yahoo.com` 200 + `features.mail`/sky.mail.
- **Split de infra observado:** maioria **ATS**; `authnapi.login` = **Google Cloud CDN**; `billing/edit.finance` = **AWS CloudFront**; `alpha/stage.api.authnapi` = 500 (app erra). Fronteiras entre stacks = costura pra testar (Q5/Q3 do The Mind).

### 🆚 Comparativo com `recon externo/` (2026-09-07) — o que ELES acharam e a gente NÃO
O recon externo (com API keys: **53.627 subs** vs meus 40 sem key) revelou superfície viva que meu Nível 1 capado perdeu. Corpus mesclado agora em `recon/all_subs_merged.txt` (53.997). Adotado em `recon/ext_{payment,oauth,admin}.txt`. **Delta acionável:**
- 🔥🔥 **ECOSSISTEMA DE PAGAMENTO INTEIRO (perdido — 74 hosts, `ext_payment.txt`):** `payments.yahoo.com`/`payment.yahoo.com` · **`token-service.payment.yahoo.com`** (dev/stg, AWS ELB) · **`subscriptions.payments.yahoo.com`** (+gdpr/mobile) · **`origin.checkout.yahoo.com`** (dev/qa/stage/prod — **AWS ELB, origins DIRETAS atrás do CDN**) · **`oidc.checkout.{yahoo,finance,mail}.com`** (OIDC no checkout!) · `carta/vertexlite/datacollector/searchads/fzdata.payments`. Quase tudo **403/Access Denied** (auth-gated, vivo).
- 🔥 **OAUTH API dedicada (perdido — 19, `ext_oauth.txt`):** `api.oauth.yahoo.com`, **`sapi.oauth2.yahoo.com` (HTTP!)**, `acctapi.login`, `umapi`, `partner.login` + envs stage/trunk/perf/canary.
- 🔥 **ADMIN panels (perdido — `ext_admin.txt`):** `admin.nevec.yahoo.com`, `mario-admin.nevec` (+b-/sb-), `hermes-admin.ec.yahoo.com`, `hk.admin.deals.yahoo.com` (403).
- **in-scope wildcard extra (48):** `credstore-stage-staging...omega.corp`, `cloudboot-auth-api-production...omega.corp`, `kubeingress.*` (internos/corp — provável inalcançável, como os outros omega).
- **Onde EU ganhei:** 72 in-scope wildcard vs 58 deles (crt.sh me deu extras) — merge cobre os dois.
- **Veredito:** o externo **não achou bug** (tudo 403/500), mas mapeou **onde o dinheiro mora** — o que muda os leads abaixo.
