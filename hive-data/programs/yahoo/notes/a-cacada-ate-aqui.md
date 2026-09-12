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
# Yahoo — a caçada até aqui (log datado)

## 2026-09-07 (sessão hunter — L7 morto in-band, nova superfície: Lightyear CMS / Creators)

### Rodou
- **L7 routing-SSRF (Y1 / CHAIN-A — a "joia da coroa") — via socket TLS cru (= Burp Repeater):**
  - Primitivas contra edge ATS `apis.mail.yahoo.com`: baseline `404 Not Found`; **Host-override → `404 Not Found on Accelerator`** (Host não-remapeado, NÃO roteado); absolute-URI / @-notation / port-confusion → **`400 Invalid HTTP Request`** (parser estrito).
  - **Oráculo de remap por Host** (bananastand gq1/bf1/bf2/ne1/sg3/tw1/corp + credstore interno + localhost + 169.254.169.254): **TODOS** `404 Not Found on Accelerator`. Só o Host próprio remapeia.
  - **Sistêmico:** repetido em `data.mail`, `proddata.xobni`, `s.yimg`, `finance`, `checkout.fantasysports` → todos `Not Found on Accelerator` p/ Host bananastand. `onepush.query.yahoo.com` **não resolve** (NXDOMAIN).
  - **VEREDITO L7 = [NEGATIVO in-band]:** ATS roda `remap_required=1` **Yahoo-wide** — nenhum edge proxeia Host não-allowlistado. Absolute-URI/@/porta morrem no parser. Resíduo único = **detecção cega via Collaborator OOB** (tarefa manual Burp; improvável com remap_required). **CHAIN-A cai** (perdeu o caminho de prova in-band).
- **CVE-2025-29927 (Next.js middleware bypass via `x-middleware-subrequest`):** testado em `cm-ui/your-content` (já 200 unauth, sem diff) e `subscriptions.payments` (307 no edge/Envoy, não middleware) → **[NEGATIVO]** (gates não são middleware Next.js).

### 🆕 ACHADO DE SUPERFÍCIE (novo top lead) — Lightyear CMS / Yahoo Creators
- `cm-ui.yahoo.com` (308 → `/your-content`) = **"Lightyear CMS"**, Next.js App Router, **200 unauth (shell), SEM CSP**. Staging gêmeo: `cm-ui.staging.yahoo.com`.
- **Bundle JS (público) vaza o backend + modelo de authz** (chunks em `/_next/static/chunks/`):
  - APIs: `cm-auth-service.yahoo.com` (**FastAPI/Python** — `{"detail":"Not authenticated"}`) · `content-service.yahoo.com` (**AWS API Gateway** — `{"message":"Missing Authentication Token"}`) · `feed-api.yahoo.com` · `api.creators.yahoo.com` (**Spring Security OAuth2** — RFC-7807) · `api.yahoo.com/{content-management,partner-portal-management,yahoo-creator-management}` (**DNS INTERNO — inalcançável de fora**).
  - **Auth = `Authorization: Bearer <access_token>`** com token de **`localStorage.getItem("access_token")`** (JWT, claim `aud`); refresh on 401.
  - **🔑 `getActiveTeamLS` / `setActiveTeamLS`** = time ativo vem do **localStorage (cliente controla)** → `activeTeam`/`activeTeamPropertyIds`/`activeTeamProviderIds`. **= candidato BOLA/cross-tenant** se o backend confia no team id sem checar membership (Pergunta #2 do The Mind).
  - Permissões: `manage:authors`, `read:user`, `CONTENT.PUBLISH`, `SECURE_PREVIEW`; mapa por API base.
  - Rotas admin (frontend): `/admin/{user,teams,brands,authors,providers,sources,properties,holding-pen}` · `/assignments/me/team/{id}` · `/assignments/filter` · `/earnings` ($) · `/content-performance/*`.
- **OAuth2 client dos Creators (capturado, unauth):** `client_id=ASWQMYXpYIZlSogi`, `redirect_uri=https://api.creators.yahoo.com/auth/callback`, scope `openid profile email`, **PKCE(S256)+state+nonce** (hardened), `activity=creator-onboarding` → **onboarding provavelmente self-serve**.
- Unauth: `cm-auth-service/assignments/me/resources` → **403 "Not authenticated"**; `content-service` → **403** (AWS API GW); `api.creators/` → **302 `/oauth2/authorization/yahoo`**.

### Decisões
- Joia antiga (L7 SSRF) morta honestamente in-band; joia nova (CMS multi-tenant) é **fit máximo BAC/IDOR** — bloqueada só por um **access_token de conta CMS/creator provisionada**.
- Split de infra (FastAPI ↔ AWS API GW ↔ Spring) entre os 3 serviços do CMS = costura de authz p/ testar (mesma ação por caminhos diferentes — Pergunta #3).

### ⛔ Bloqueio p/ retomar
**Preciso de um `access_token` (JWT) válido do Lightyear CMS / Yahoo Creators.** Passo do Tiago: logar/onboard a conta `moldret_intigriti@yahoo.com` em `cm-ui.yahoo.com` (ou o dashboard de creators) → abrir DevTools → `localStorage.getItem("access_token")` → colar num cookie/tokenfile. Aí rodo os testes autenticados de BOLA (trocar `activeTeam`/team-id/user-id entre contexto próprio e alheio) via `tools/yahoo_auth_probe.py`.

### Próximo passo (concreto)
1. **[precisa Tiago]** Onboard/login em Yahoo Creators c/ a conta de teste → extrair `access_token` → testar **BOLA no `activeTeam`/`/assignments/me/team/{id}`/`/admin/user/{id}`** (2 contas ou 1 conta + IDs incrementais). Fit #1.
2. **[unauth, executável já]** Testar validação de `redirect_uri` do client `ASWQMYXpYIZlSogi` no `api.login.yahoo.com/oauth2/request_auth` (troca de redirect_uri/subdomínio) — lead OAuth secundário.
3. **[unauth]** Enumerar rotas do `cm-auth-service` (FastAPI → tentar `/docs`,`/openapi.json`) e `content-service` (AWS API GW → paths do bundle).

### Adendo (mesma sessão) — protocolo `descobrir a roda` executado
- Reconstruído o modelo do sistema (edge ATS/remap · IdP OAuth central · subsistema Lightyear CMS com 3 backends: FastAPI/AWS-API-GW/Spring) + 6 fronteiras de confiança (B1–B6 + B-Next) em `arapuca.md`.
- Derivadas 5 cadeias narrativas (E/F/G/H/I) + mapa do não-testado ranqueado.
- **Chains unauth executadas e MORTAS:** CHAIN-H (sem proxy interno; só `/api/log`) e CHAIN-I (origins PCI-CDE alcançáveis mas ELB→403 sem mTLS do edge; peça Y28).
- **Desfecho:** as 3 chains de maior impacto (E activeTeam-swap / G content-service-direto / F aud-carryover) dependem **só do access_token JWT**. Yahoo em espera do token (frente unauth exaurida).

### Verificação final "não deixar nada pra trás" (mesma sessão)
- Fechei 2 gaps unauth que estavam marcados-mas-não-executados: **C-03/Y16 header-replay = NEGATIVO** (serviços ignoram identidade forjada) e **WCP unkeyed-fuzz = NEGATIVO** (CMS não-cacheado).
- **Frente unauth do Yahoo agora 100% exaurida.** Tudo restante = atrás do token (CHAIN-E/G/F + privesc) ou deferido por rate-limit (OAuth `activity=`, dirty-dancing) — registrado na arapuca.
- Superfície autenticada pré-mapeada e pronta (mapa de endpoints + RBAC + plano de 6 provas). Retomada = colar `access_token` → disparar.

## 2026-09-08 (sessão hunter — OSINT passivo: modelo de auth do CMS corrigido)

### Rodou (100% passivo — só Internet Archive CDX + web/GitHub search; zero toque ativo na Yahoo)
- **Higiene de escopo:** `scope.md` do Yahoo estava com o **template `*.exemplo.com`** → o `scope_hook` não enforçava NENHUM OOS real. Preenchido com o escopo da política (in/out reais); validado que `.yahoo.com.tw`/`.tumblr.com`/`.vdms.com` → OUT_OF_SCOPE, bananastand → IN_SCOPE.
- **Wayback CDX (`web.archive.org`) nos hosts do CMS:** `cm-auth-service.yahoo.com` = **237 URLs arquivadas**; `cm-ui.yahoo.com` = 798 (só `/_next/` chunks — SPA). `api.creators`/`content-service`/`feed-api`/`creators.yahoo.com` = 0 arquivado. Cru em `recon/osint/cdx_*.txt`.
- **Web/GitHub search** por `cm-production-yahooinc.us.auth0.com` / `cm-auth-service.yahoo.com` / `app_id=lightyear` / `ASWQMYXpYIZlSogi` → **nenhum segredo/client_id/config vazado** (bom sinal do programa; sem jackpot). Confirmou `creators.yahoo.com/sign-in` = front público de cadastro → **onboarding self-serve** (Tiago consegue provisionar a conta).

### 🔑 CORREÇÃO DO MODELO DE AUTH (o achado da sessão)
As URLs `/login?referrer_url=...` arquivadas revelam que o Lightyear CMS **não** usa o Yahoo IdP/Spring que o arapuca assumia — usa **Auth0**:
- **IdP real = Auth0 tenant `cm-production-yahooinc.us.auth0.com`.**
- **cm-auth-service** = backend de troca de código; rotas **`/login` · `/logout` · `/token`**.
- Fluxo: `cm-auth-service.yahoo.com/login?referrer_url=<https://cm-ui.yahoo.com/your-content?...&code=<AUTH0_CODE>&state_token=<...>>&app_id=lightyear` → **307**.
- Os `code=`/`state_token=` capturados são de snapshots abr–jul/2026 = **códigos single-use expirados** (inúteis como cred viva).

### 🎯 Novo lead [UNTESTED] (unauth) — Y-OSINT-1: validação de `referrer_url`
`cm-auth-service.yahoo.com/login` recebe `referrer_url` (onde o `code`/token aterrissa pós-login). **Se não valida o host** (permite host arbitrário/subdomínio/aberto) → **vaza o código de auth p/ atacante** (leak estilo redirect_uri, mas no handler de login do próprio app). **Diferente** do teste de redirect_uri do client Yahoo-IdP `ASWQMYXpYIZlSogi` que já deu estrito — este é o caminho **Auth0/cm-auth-service**, nunca testado. Probe ativo = `[REQUER AUTORIZAÇÃO]` (delegado ao Tiago abaixo).

### Delegação ao operador (probes ativos evitados nesta sessão passiva)
1. **[Tiago — destrava fit #1]** Onboard `moldret_intigriti@yahoo.com` em `creators.yahoo.com/sign-in` → autorizar no CMS → DevTools → `localStorage.getItem("access_token")` → me passar o JWT. Aí disparo CHAIN-E/F/G (BOLA no `activeTeam`).
2. **[probe ativo, precisa header X-Bug-Bounty]** Testar `referrer_url` no `cm-auth-service/login`: `?referrer_url=https://<host-atacante>&app_id=lightyear` vs baseline `cm-ui.yahoo.com` — ver se 307 segue p/ host externo ou rejeita. Barato, unauth, alto valor se frouxo.
3. **[terceiro/OOS — NÃO tocar]** Auth0 tenant `cm-production-yahooinc.us.auth0.com` (open signup / `.well-known` / connection enum): é infra `auth0.com` + nome `yahooinc` (⚠️ `*.yahooinc.com` é OOS) → fora de escopo; só noto como intel.

### Próximo passo
- **Se Tiago colar o token:** CHAIN-E/F/G (arapuca) + agora também mirar o **/token do cm-auth-service** (troca de código Auth0 — testar reuso/aud/troca de client).
- **Autônomo (item 1 do log anterior):** replicar bundle-harvest→oráculo nos outros apps Yahoo vivos.
- **Ativo unauth barato:** lead Y-OSINT-1 (referrer_url) quando religar `formação de lança`.

## 2026-09-08 — Selo de saída (encerramento hunter+dojo)

### Relatório sincero: o que dá pra fazer ANTES do token (destravar)
**Frente unauth do CMS: EXAURIDA — tudo negativo/bloqueado testado:**
routing-SSRF (remap_required) · direct-origin ELB (403 mTLS) · header-forge de identidade (ignorado) · WCP (não-cacheado) · middleware-bypass CVE-2025-29927 (auth no edge) · `_next/image` SSRF (allowlist estrita) · docs/actuator (gated) · OAuth redirect_uri (estrito). **A borda/auth do Yahoo está bem-configurada contra os truques unauth — o risco residual está na LÓGICA de authz atrás de credencial válida.**

**Ainda dá pra fazer sozinho (EV honesto, NÃO feito ainda):**
1. **[EV alto] Replicar o método `bundle-harvest → oráculo de API` nos OUTROS apps Yahoo vivos** do recon (admin panels `mario-admin.nevec`/`admin.nevec`/`hermes-admin.ec`/`hk.admin.deals`; fantasy `football.`/`beacon.`; ~51 hosts vivos do Nível 3). Acabou de render forte no cm-ui — outro SPA pode ter auth mais fraca ou endpoint aberto. **Melhor uso do tempo sem o Tiago.**
2. **[EV médio] OSINT passivo** (GitHub code search / gau / wayback) por token/segredo/endpoint vazado — `client_id ASWQMYXpYIZlSogi`, `cm-auth-service`, `lightyear`, `content-service.yahoo.com`. Um token vazado destrava TUDO.
3. **[EV baixo] Server Actions** do cm-ui (`Next-Action: <id>` POST) — último vetor app-level vivo; mutante + baixa prob + cm-ui em 429 agora.

**Limite honesto:** o achado de verdade (BOLA/privesc/lógica no CMS) está **atrás do token**. Nenhuma esperteza unauth substitui a credencial — testei os bypasses e todos seguraram. Sem token ou credencial vazada, eu **mapeio** (armo a fase autenticada) e **compro bilhete de loteria** (leak OSINT / endpoint aberto em outro app), mas não produzo o BOLA.

### Estado ao encerrar
- **Alvo:** Yahoo (Intigriti). **Onde parou:** superfície unauth exaurida; joia = Lightyear CMS multi-tenant, 100% mapeada, travada no `access_token` JWT.
- **Próximo passo (2 caminhos):** (a) Tiago cola o `access_token` → disparo CHAIN-E/F/G (probes prontas na arapuca); (b) autônomo: método bundle→oráculo nos outros apps Yahoo (item 1 acima).
- **Aprendizado persistido:** `S-ORACLE-01` (arsenal) + `S-JWT-01`/`S-NEXT-01`/`S-APIGW-01`/`S-BOLA-TENANT-CLIENT-01` (dojo) + FP `remap_required` + método de headers.
- Dojo fechado; revisões espaçadas agendadas (08/09 → 14/09 → 07/10).

## 2026-09-08 (sessão hunter pt3) — PAUSA sem Selo formal (a pedido do Tiago)

**Nota:** sessão pausada explicitamente sem rodar `seal_gate.py`/`code 7 send`. Este bloco é só
um checkpoint de continuidade, não o Selo de saída completo.

### Fantasy checkout ($-logic) — mapeado, superfície de assinatura fail-closed
- Fantasy Plus/Ultra geo-bloqueado no web (só app); testado via Yahoo Mail Plus (mesma API
  `subscriptions.payments.yahoo.com`, Next.js RSC). Preço/plano resolvido **server-side** a
  partir do `sku=` puro — sem `estimate` client-editável. **FC-2 (mass-assign preço) REFUTADO.**
- Endpoints mapeados, não testados: `freetrial/{eligibility,purchase}` (FC-6),
  `coupons/isEligibleForAccount` vs `purchase` (FC-7), `guestcheckout/*` (FC-5, falta achar
  entrypoint real).

### `descobrir a roda` r2 → CHAIN-K testada e MORTA
Hipótese de usar o próprio checkout como "ralo" pra fechar o L1 da CHAIN-J: rota antiga
`confirmation`/`ompc.referer` está 404 (descontinuada); `errors?error=` não existe; reflection
observada é só eco inofensivo do Next.js App Router. **CHAIN-J segue [PARCIAL]**, sem progresso
novo no L1.

### CHAIN-M (mass-assignment write Fantasy) — bloqueada por 2 motivos estruturais
1. Liga de teste `moldret's Matchless League` (1620882) sem draft feito → sem roster →
   `roster_actions` não testável ainda.
2. No form de team-settings (`editteaminfo`), **crumb CSRF validado corretamente** (fail-closed)
   e há indício forte de enforcement de `Sec-Fetch-Mode:navigate` (fetch/XHR programático
   rejeitado mesmo com payload byte-idêntico ao form nativo) — bloqueia teste de
   mass-assignment via automação pura. Tentativa de injetar campo extra no form real +
   clique físico foi barrada pelo harness desta sessão (safety check local, não do Yahoo) —
   **`[NÃO TESTADO: mass-assignment via campo extra — bloqueio de ferramenta]`**, não negativo.

### Próximo passo (quando retomar)
1. **B12** — recon de Wallet/PicknWin/Best Ball (dinheiro real de aposta, in-scope, zero
   recon feito — maior lacuna identificada).
2. Esperar o draft da liga de teste (fecha 09/09 17:20 PT) → reabre CHAIN-M com roster real.
3. Retomar chain-search do L1 da CHAIN-J por outro ângulo (hosts produto/omega frescos).
4. Se Tiago colar `access_token` do CMS → CHAIN-E/F/G prontas na arapuca.

## 2026-09-12 — sessão hunter: B12 (superfície de dinheiro Fantasy) — recon executado

**Frente:** as superfícies de dinheiro real (Best Ball / PicknWin / Daily Fantasy / Wallet)
que estavam com **zero recon** (maior lacuna, item #1 do próximo-passo anterior).

### Liveness (serial, unauth, header setado)
- ✅ **bestball.fantasysports.yahoo.com** — VIVO 200 (ATS+Envoy), React `tdv2-app-fantasy`.
- `dailyfantasy.yahoo.com` — 301 → sports.yahoo.com/dailyfantasy (só redirector).
- `game.penaltyshootout.yahoo.com` — resolve mas **cert SNI mismatch** (AWS 13.248.158.7) — parked/deprecated.
- `api.penaltyshootout` · `api.bestball` · `dev.wallet` · `wallet.secure` — **NXDOMAIN** (mortos).

### Achado de recon (forte): catálogo fetchr do app fantasy unificado
Método `bundle-harvest → oráculo` (S-ORACLE-01). O state de hidratação do bestball expõe o
**catálogo fetchr completo** (superset compartilhado dos apps fantasy). xhrPath real =
`/tdfan/api/resource/<service>;<params>`. Catálogo salvo em
`recon/bestball-fantasy-fetchr-catalog.md`.

### Wall unauth (diferencial §6) — inconsistente entre wallet.*
- `bestball.myLeagues` → **401** (fail-closed) · `wallet.subscriptions` → **200 vazio** (fail-open shape)
- `wallet.bethistory`(+`guidOverride`) / `bethistorysummary` → **503 request-failed** = rota
  **registrada e alcança o backend BetMGM**, só falta sessão · `wallet.product` → 500.
- `fantasyread.*`/`tourney.*`/`progrss.*` → 400 "not registered" (registro é por-host).

### Leads (ranqueados) — §4: NENHUM impacto demonstrado; todos atrás de credencial
1. **[LEAD #1 · money+PII] `wallet.bethistory` + `guidOverride`** — param client-controlado
   literalmente "override do guid" em endpoint de histórico de apostas. PoC autenticado armado abaixo.
2. **[money] `tourney.*`** (host `tournament.`, auth-walled 302/signup): bracketPicks/group* por
   `teamId`/`groupId` (+ `*Key` — checar se opcional = IDOR); `smartAdsTeam.useLogin` = toggle auth §7.
3. **[PII] `progrss.contacts`** `/user/{guid}/contacts;out=name,email,image` cross-user.
4. **[logic] `graphite.bettingRestriction`** `userStateAbbr` client-controlado → bypass geo de aposta.

### PoC autenticado ARMADO — lead #1 (para Tiago / `code -1`)
Marcador-primeiro (§3.1 p3), read-only, não-destrutivo. Trocar `<CRUMB>`+cookies pela sessão moldret:
```
# baseline: MEU histórico (resolve pela sessão)
GET /tdfan/api/resource/wallet.bethistory;count=5?format=json&crumb=<CRUMB>
# probe A (marcador que não casa nada): guidOverride diferencial
GET /tdfan/api/resource/wallet.bethistory;guidOverride=ZZFAKEGUIDMARKER0000;count=5?format=json&crumb=<CRUMB>
# probe B (guid REAL de 2ª conta): se retorna as apostas DELA -> BOLA confirmado (par read-back §4)
GET /tdfan/api/resource/wallet.bethistory;guidOverride=<GUID_2A_CONTA>;count=5?format=json&crumb=<CRUMB>
Host: bestball.fantasysports.yahoo.com  ·  Header: X-Bug-Bounty: Intigriti-moldret
```
Interpretação: baseline 200-com-dados vs probe-A (400/403/vazio = validado) vs probe-B (dados
alheios = BOLA). §4: só crava com leitura-de-volta que prove o dado de outra conta.

### Próximo passo
1. **[Tiago/code -1]** rodar o PoC armado do lead #1 (`guidOverride`) → confirma/refuta BOLA money+PII.
2. **[autônomo]** minerar chunks lazy-loaded do bestball (rotas Draft/LeagueJoin = entry-fee) por
   mais services + lógica de entry-fee; repetir bundle-harvest em `football.`/`golf.`/`beacon.` fantasy.
3. **[Tiago]** logar no `tournament.` → capturar xhrPath real + crumb → testar `tourney.*` BOLA (brackets pagos).
