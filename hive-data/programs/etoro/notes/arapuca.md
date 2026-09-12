---
program_id: etoro
document_type: note
claimed_scope_status: unknown
classification: internal
source: bugcrowd policy (etoro) — 2026-09-09
collected_at: 2026-09-12T00:00:00Z
tags: [etoro, root]
asset_refs: [etoro.com]
---
# arapuca.md — eToro · peças pra chain 🪤

`protocolo armar a arapuca`. Fonte: openapi.json (v1.367.0, 142 paths) + llms.txt, lidos no `modo hunter` (2026-09-05).
Cada peça = endpoint com **path-param de recurso** → candidato a BOLA/IDOR pela **Q1 do The Mind** ("quem checa o dono?").
**Status geral:** todas `[UNTESTED]` — testar exige conta eToro + **User Key** (Settings>Trading>Create Key, e-mail `@bugcrowdninja.com`); BOLA cross-account precisa de **2 contas**. Header `X-Bug-Bounty: moldret` em tudo.

## Modelo de auth (o cerne)
2 chaves: **Public API Key** (app) + **User Key** (conta). Token v2 delegado: `POST /api/v2/agent-portfolios/{agentPortfolioId}/user-tokens` com **`scopeNames` no body**. Scopes permitidos: `GET /api/v2/agent-portfolios/user-tokens/scopes`.
→ 2 suposições atacáveis: (Q1) o servidor checa que EU sou dono do `{agentPortfolioId}`/`{accountId}`/`{clientId}`? · (Q2) o servidor limita os `scopeNames` que eu peço?

## Peças (por valor)

### 💰 Dinheiro / saldo — BOLA de leitura (crown jewels)
| id | endpoint | param | Q | o que HABILITA |
|----|----------|-------|---|----------------|
| **E1** | `GET /api/v1/balances/{accountType}/{accountId}` (+`/history`) | `accountId` | Q1 | ler saldo de OUTRA conta; `accountId` enumerável → exfil em massa |
| **E2** | `GET /api/v1/money/accounts/cash/{accountId}/transactions` | `accountId` | Q1 | extrato/transações de terceiro |
| **E3** | `GET /api/v1/money/transfers/{transferId}` | `transferId` | Q1/Q3 | detalhe de transferência alheia (valores, contrapartes) |

### 🔑 SSO / OAuth app — BOLA de escrita (maior impacto)
| id | endpoint | param | Q | o que HABILITA |
|----|----------|-------|---|----------------|
| **E4** 🔥 | `POST /api/v1/sso/applications/{clientId}/client-secret` | `clientId` | Q1 | **regenerar o secret de um OAuth app alheio** → impersonar o app → ATO dos usuários dele |
| **E5** | `PUT,DELETE /api/v1/sso/applications/{clientId}` | `clientId` | Q1 | editar/deletar app SSO de terceiro |

### 🔐 Token de agente — BOLA + privesc de scope (o mais profundo)
| id | endpoint | param/body | Q | o que HABILITA |
|----|----------|-----------|---|----------------|
| **E6** 🔥 | `POST /api/v2/agent-portfolios/{agentPortfolioId}/user-tokens` | `{agentPortfolioId}` + `scopeNames` | Q1+Q2 | criar token num agent-portfolio que não é meu, e/ou pedir scope elevado (trade/withdraw) → agir sobre fundos alheios |
| **E7** | `PATCH /api/v2/agent-portfolios/{agentPortfolioId}/user-tokens/{userTokenId}` | ambos | Q2 | escalar scope de um token existente |
| **E8** | `DELETE /api/v1/agent-portfolios/{agentPortfolioId}/user-tokens/{userTokenId}` | ambos | Q1 | revogar token de terceiro (DoS de integração) |

### 📉 Trading — BOLA de escrita em posição/ordem REAL (não-demo)
| id | endpoint | param | Q | o que HABILITA |
|----|----------|-------|---|----------------|
| **E9** 🔥 | `POST /api/v1/trading/execution/market-close-orders/positions/{positionId}` | `positionId` | Q1 | **fechar a posição REAL de outro usuário** → dano financeiro |
| **E10** | `PATCH /api/v2/trading/positions/{positionId}` | `positionId` | Q1 | alterar SL/TP de posição alheia |
| **E11** | `DELETE /api/v2|v3/trading/execution/orders/{orderId}` | `orderId` | Q1 | cancelar ordem pendente de terceiro |

### 🧩 Lógica — demo↔real (Q3: caminho alternativo)
| id | descrição | Q | o que HABILITA |
|----|-----------|---|----------------|
| **E12** | Todo endpoint de execução existe em par `/demo/` e real (`market-open/close-orders`, `orders/{id}`, `positions/{id}`). | Q3 | token com scope **demo** aceito no path **real**? → trade real com auth de demo |

### 🔀 www BFF — header `AccountType` cliente-controlado (o caminho vivo)
| id | endpoint | sinal | Q | o que HABILITA |
|----|----------|-------|---|----------------|
| ~~**E16**~~ | `GET www.etoro.com/api/logindata/v2/logindata` — header `AccountType` | Q2/Q3 | **`FALSO POSITIVO` (contra-prova 2026-09-05).** Teste decisivo: toggle Demo/Real **nativo da UI** troca normalmente e muda os dados igual à API — confirma que é o **fluxo legítimo**, não bypass. `Cid` diferente = shell interno Real/Demo da mesma conta (by-design), não BOLA cross-user. **Peça morta** — não retestar. |

### 🕵️ MCP gateway — rotas escondidas do portal público (P7)
| id | achado | severidade | próximo elo |
|----|--------|-----------|-------------|
| **E17** | `mcp.public-api.etoro.com` (POST /, JSON-RPC, **sem credencial**) expõe catálogo com **191 rotas**; openapi.json público (`api-portal`) só documenta **172**. Diff = **12 rotas reais de execução de trade** ausentes do portal: `market-open-orders/{by-amount,by-units,{orderId}}`, `limit-orders` (+ `/demo/`), `trading/info/{real,demo}/orders/{orderId}`. **Testado:** `execute-write` anônimo na rota escondida → **401 + WWW-Authenticate, mesmo enforcement das documentadas.** Sem bypass demonstrado. | **Low/Info** — disclosure de superfície, não authz quebrado | testar **diferencial de scope** com credencial real: um token/key escopado só pra rotas documentadas alcança as escondidas? (precisa API key funcional — bloqueado até desbloquear onboarding) |

**Técnica nova (persistir no arsenal):** servidor MCP que expõe `get-tags`/`get-all-routes` sem credencial = oráculo de **contagem de rotas** grátis. Diffar `totalRouteCount` do MCP contra o openapi.json público revela rotas não-documentadas sem tocar nenhuma delas.

### 🗂️ Social / watchlist — BOLA de escrita (menor valor, mas real)
| id | endpoint | param | o que HABILITA |
|----|----------|-------|----------------|
| **E13** | `PUT,DELETE /api/v1/posts/{postId}` | `postId` | editar/deletar post alheio |
| **E14** | `PUT,DELETE /api/v1/watchlists/{watchlistId}` (+`/rank`,`/setUserSelectedUserDefault`) | `watchlistId` | mexer na watchlist de terceiro |
| **E15** | `.../posts/{postId}/comments/{commentId}` PUT/DELETE · `/polls/{pollId}/options/{optionId}/votes` | ids | editar comentário alheio; fraudar enquete |

### 👤 User-info por username (provável PÚBLICO POR DESIGN — FP a antecipar)
`GET /api/v2/portfolios/{username}/{copiers,gain,rankings}` · `/user-info/people/{username}/{portfolio/live,tradeinfo,gain}` → eToro é social; portfólio de trader público é **público por design**. **Só vira bug** se expuser conta **privada** ou dado não-consentido. Testar com conta privada.

## Chains candidatas (passo de combinação)
- **C1 — exfil financeiro em massa** = E1 + E2 (+ `accountId` enumerável). BOLA de saldo → enumerar → PII/financeiro de milhares. *Elo:* `accountId` previsível/enumerável.
- **C2 — takeover de OAuth app** = E4 (regen secret alheio). Isolada já é Critical-shape (impersona o app → ATO dos usuários). *Elo:* `clientId` de app alheio (é público no fluxo OAuth).
- **C3 — sequestro de fundos via token de agente** = E6/E7 (criar/escalar token em agent-portfolio alheio com scope de trade/withdraw). *Elo:* provar que `{agentPortfolioId}` alheio é aceito.
- **C4 — dano em posição real** = E9/E10/E11 (fechar/alterar/cancelar ordem alheia por id). *Elo:* `positionId`/`orderId` enumerável ou vazado.
- **C5 — demo→real** = E12 (token demo no path real). *Elo:* confirmar que o gateway não separa scope demo/real por path.

**Melhor chain agora:** **C2 (E4)** — maior impacto isolado (OAuth app takeover), menos elos faltando (só precisa de um `clientId` alheio, que é público). Depois **C3 (E6)** — o modelo de delegação é novo e complexo = onde authz costuma furar.

## Bloqueio pra executar (estado real, 2026-09-05 fim de sessão)
- **Conta:** real, não-demo, `moldretvalor726333@bugcrowdninja.com` (user `moldretvalor7284`, `gcid 49462268`). Onboarding travado em **verificação de telefone** — endpoint rate-limited (Cloudflare 1015, ~23h). Sem isso: sem KYC, sem API key formal, `Credit: 0`.
- **Scope wall confirmado (6/6 testes):** o bearer de sessão web autentica em `public-api.etoro.com` **e** em ambas as MCPs, mas **sem nenhum scope** (`403 InsufficientPermissions` consistente, direto e via MCP-relay). A MCP é proxy honesto — sem bypass.
- **Caminho vivo:** o mesmo bearer tem **poder total** no `www.etoro.com/api/*` (BFF) — contexto de conta completo, sem gate de scope (só sessão). É ali que BOLA de verdade mora, não na Public API.
- Ideal **2 contas** pra BOLA cross-account confirmado. Muitos BOLA de leitura/escrita (E1/E9/E13-15) dá pra sondar com **1 conta** via BFF (Q1 sem 2ª conta: id que não é meu → `200/sucesso` vs `403/erro`) — nível 1 do `protocolo One Piece`. **Toda chamada autenticada exige o Tiago rodar** (regra de delegação do escudo) — eu desenho o comando, ele executa com a própria sessão.

---

# `por.etoro.com` — Portal de Afiliados (Surface nova, 2026-09-06)

## Descoberta
SPA Angular servida em `por.etoro.com` (host in-scope `etoropartners.com`? redirecionado). Estáticos em **S3**, auth via **Azure AD B2C** (tenant `etoropartners.onmicrosoft.com`).

## Backend mapeado
- **Base:** `https://affapi.etoro.com/api/` (API .NET/Azure App Service, Cloudflare edge).
- **Logger:** `etorologsapi.etoro.com/api/v2/monitoring`.
- **Auth:** Azure AD B2C policy `B2C_1_signin` (OAuth2 implícito + code habilitados).

## Endpoints achados (v1/*, todos `affapi.etoro.com`)
| Serviço | Rotas | Impacto |
|---|---|---|
| **Affiliates** | `/info`, `/parent`, `/register/{private,corporate}`, `/exists?email=` | **P2-P3**: enumeração de e-mail + validação de registro |
| **ib/Traders** | `/{id}` (by ID), `/` (list com filtros tier/verificationLevel), `/generateExcel` | **🔥 P1**: BOLA de trader alheio — dados de copier/portfolio |
| **Billing** | `/balance`, `/Payments/{status}`, `/payments/latest`, `/payments` | **P2**: financeiro de afiliado (mas sem id no path = scoped à sessão) |
| **Plans** | `/info`, `/byCountry/{countryCode}` | auxiliar |
| **Media/Links** | `/banners`, `/` (generateLink com bannerId) | marketing |
| **Compliance** | `/documents` (GET all, POST sign) | KYC |
| **Performance** | `/v1/` + `/v2/` (getTiersData) | comissão/tier |
| **Countries** | `/my`, `/`, `/restricted` | listas + geoblock |
| **KYP/IBAN** | `/status`, `/kyp/iban` (validate) | onboarding |

## Achados críticos

### 🔥 **E18** — BOLA em `GET /v1/ib/Traders/{id}` (a joia)
**Path-param não-validado:** trocar `{id}` por outro → dados de trader alheio (username, portfolio gain, copyTrading status, verification level, tier).
- **Status:** `[UNTESTED — precisa Bearer B2C]`
- **P:** P1 (PII + financeiro de terceiro em leitura)
- **Elo:** id é enumerável (inteiro sequencial, via list com `/traders?...`)

### **E19** — `POST /v1/Affiliates/exists?email={email}` enumeração
Valida se e-mail já está registrado. Header **sem** `"no-intercept":"true"` = pode estar atrás de auth.
- **Status:** `401 Unauthorized, www-authenticate: Bearer` (confirmado 2026-09-06 01h52 UTC)
- **P:** P4/OOS (fora de escopo — enumeração isolada) = paga baixo ou não paga
- **Chain:** elo de phishing/account takeover se combinado c/ signup password reset

### **E20** — `POST /v1/Affiliates/register/{private,corporate}` auth-gated
Endpoint de autenticação enforçada (401 + Bearer).
- **Status:** `401 Unauthorized, www-authenticate: Bearer` (confirmado)
- **Contexto:** Signup via B2C **contorna o muro do telefone** da conta trading principal → potencial step-skip se B2C não validar pré-requisitos de KYC. **[BLOQUEADO — signup UI complexa, não testado]**
- **Impacto:** Se conseguir registrar, ganha Bearer → acesso a E18 (BOLA ib/Traders)

## B2C OAuth2 (mapeado, não testado por segurança)
**Tenant:** `c6474d1e-3509-488f-856a-0b8bc00f4be3`
**Policy:** `B2C_1_signin` (há também `B2C_1_signup`, mas não visível no `.well-known`)
**Response types:** `code`, `token`, `id_token token` (fluxo implícito **habilitado** — risco de redirect_uri frouxo)
**Scope:** `https://etoropartners.onmicrosoft.com/partnersapi/user_impersonation`
**Claim custom:** `extension_DBId` (id interno do afiliado no DB)

⚠️ **Técnica:** Azure-AD B2C redirect_uri frouxo / tenant confusion / cross-tenant — já no arsenal; aplicável aqui se escopo expandir pro B2C (hoje OOS).

## Status final
- **E18 (ib/Traders BOLA):** Maior impacto, enumerável, auth-gated (precisa Bearer). **[UNTESTED, REQUER Bearer B2C]**
- **E19/E20 (enumeração + registro):** Bloqueados por auth; signup UI é barreira prática.
- **Ramo vivo mas difícil de testar sem contorno de signup.** Candidato pra **segunda sessão** se escopo/contexto evoluir.


---

# `seguir o rastro` — 2026-09-05

**Sinal-raiz:** o bearer de sessão web tem poder total no BFF (`www.etoro.com`) mas é cego (sem scope) em `public-api`/MCP. → **"quais OUTROS hosts do parque aceitam esse mesmo token, e o que cada um expõe?"**

| Ramo | Host | Resultado (anon, silencioso) | Veredito |
|---|---|---|---|
| 1 | `cashier.etoro.com` | `302 → /cashiermvc/Account/Login` (ASP.NET próprio) | **[UNTESTED]** — pode herdar cookie `Domain=.etoro.com`? Testável só **navegando** logado (sem tamper) — pede pro Tiago abrir a URL direto. |
| 2 | `kyc.etoro.com` | `200`, corpo vazio (SPA client-render) | **[UNTESTED]** — precisa JS/browser real pra ver o fluxo; baixa prioridade (toca dado de identidade, cuidado extra). |
| 3 | `goodwallet.etoro.com` | `200`, Next.js/Vercel app | **[UNTESTED]** — testar se aceita o bearer via `Authorization` (o Tiago roda). |
| 4 | `oauth.wallet.etoro.com` "OpenBanking" | `200`, SPA própria | **[UNTESTED]** — liga ao lead #2 antigo (`redirect_uri`/token OAuth). |
| 5 | `agent.public-api.etoro.com` | **Outra instância MCP** — catálogo **114 rotas** (menor que as 191 da `mcp.public-api`); `SSO-Applications:1` vs `5`, `Copy Trading:2` vs `5` | **`[PADRÃO — design sensato]`** — parece subset curado/mais restrito pra uso de agente IA (menos tools mutantes expostas). Não é bug; **mas vale diferencial**: será que `agent.public-api` aceita credencial que `mcp.public-api` rejeita, ou vice-versa? (não testado ainda) |

**Ramos que fecharam:** #5 (design intencional, não vulnerabilidade).
**Ramos vivos, precisam do Tiago (regra de delegação):** #1 (mais barato — só navegar), #3, #4 (bearer via header).

---

# `descobrir a roda` v2 — 2026-09-05 (pós scope-wall confirmado)

## O que mudou desde a v1
- ✅ **Costura #1 testada:** confirmado — BFF tem poder, public-api/MCP não (6/6 403). **Hipótese "cross-surface privesc" (N5) FALSEADA** — enforcement de scope é consistente, sem furo demonstrado.
- ✅ **Não-testado #1 (reads do BFF mid-onboarding) testado:** `logindata` serve dado (inclusive shell Real vazio) **mesmo com telefone não-verificado** — mas é **benigno** (conta vazia, sem posições/fundos, `CustomerRestrictions:[]` sem impacto acionável). Não é achado sozinho.
- ✅ **Não-testado #3 (MCP) testado:** achou **E17** (12 rotas de trade escondidas do portal público, mas com mesmo enforcement — Low/Info) + 2ª instância MCP (`agent.public-api`, subset curado).
- 🔒 **Não-testado #2 (onboarding step-skip) ainda bloqueado:** o endpoint-chave está rate-limited ~23h — não force.

## Hipóteses de teste RANKEADAS (produzidas agora, priorizando o que dá pra fazer com 1 conta via BFF, sem esperar o onboarding)

1. **🔥 [BFF · Q1 · sem 2ª conta] Owner-check em ação de escrita social.** Testar `PUT/DELETE` em `postId`/`commentId`/`watchlistId` que **não é meu** (marker id — não real de terceiro, pra não violar "não tocar em conta alheia"). Diferencial: `403/401` (auth check existe) vs `404 do mesmo jeito que id inexistente` (sugere que o check de dono só roda **depois** da existência — não prova bypass sozinho, mas orienta o próximo teste). **Comando pronto pro Tiago:** capturar 1 cURL de "editar meu próprio post" no DevTools, e eu construo a variação com id trocado.
2. **🔥 [BFF · Q3 · sem 2ª conta] `cashier.etoro.com` herda sessão?** Só navegar (logado) pra `https://cashier.etoro.com/cashiermvc/` e ver se autentica sozinho via cookie `.etoro.com`, ou pede login separado. Se autenticar → nova superfície .NET MVC pra mapear (lógica de pagamento, C7 do signals.md).
3. **[BFF · Q1] `oauth.wallet.etoro.com` com o bearer** → se aceitar, testa o modelo OAuth do wallet (liga a `redirect_uri`/token, arsenal técnica Azure-AD adaptada).
4. **[BFF · Q2] Trocar `AccountType` pra valor INVÁLIDO** (não `Demo`/`Real`, ex. `Kid`/`Agentic` — os sub-account types que a `logindata` revelou existir) — já que E16 provou que o header é lido; testar se aceita um valor de sub-conta que não deveria estar disponível ainda (`Agentic` é o modelo de delegação futuro).
5. **[baixa prioridade, aguarda telefone] N1 propriamente dito** — quando o rate-limit de ~23h passar, RETENTAR o onboarding e ver se dá pra pular a etapa de telefone chamando a próxima direto (KYC) sem completar o SMS.

**Próxima a executar quando o Tiago voltar:** #1 (não precisa nem de host novo — usa o mesmo BFF já confirmado vivo) ou #2 (1 clique, zero request manual).

---

# `descobrir a roda` — 2026-09-05 (empacou no onboarding)

## Modelo do sistema (hipótese — marcar cada inferência)
```
                         ┌─────────── EDGE ───────────┐
  cliente  ──HTTPS──►    │ Cloudflare (WAF, cf_clearance, __cf_bm, rate-limit 1015)
                         │ + DataDome (bot mgmt: cookie datadome + x-datadome-clientid)
                         └──────────────┬─────────────┘
                                        ▼
        ┌───────────── www.etoro.com/api/*  (BFF do app web) ─────────────┐
        │ auth: bearer JWE (kid 04434, dir A256CBC-HS512, claim AccountType)│
        │ + headers X-Session-Id, X-STS-DeviceId, ApplicationIdentifier     │
        │ serve: onboarding, trading, portfolio, feeds (o app real)         │
        └───────────────┬───────────────────────────────────────┬─────────┘
                        │                                        │
        ┌───────────────▼──────────┐        ┌───────────────────▼──────────────────┐
        │ public-api.etoro.com     │        │ backend services (subdomínios)         │
        │ developer API, 142 EP    │        │ wallet/custódia (BitGo/Fireblocks/     │
        │ auth: apiKey+userKey     │        │  Simplex webhooks) · oauth.wallet/     │
        │  OU oauth2 (scopes)      │        │  OpenBanking · cashier(.NET MVC) · KYC │
        │ MESMO token web é aceito │        │  · agent-portfolios (copy/delegação)   │
        │  mas SEM scope (403)     │        │  · Lightstreamer(push) · MCP servers   │
        └──────────────────────────┘        │  · bo.* / argocd / consul (CF-gated)   │
        etorologsapi (telemetria .NET/Kestrel)└──────────────────────────────────────┘
```
**Onde o estado vive:** máquina de estados da conta = `registro → e-mail ✓ → ☎️ verif. telefone [PAREDE] → KYC → fundos → trading`. Scopes do token = `{demo,real}:{read,write}`, `user-info:read`, `money.balance:read`. Demo↔Real = mesmo path com prefixo `/demo/`.

## Engenharia reversa das costuras (6 perguntas na junta)
1. **Token cross-surface (Q1/Q2):** o MESMO JWE vale em `www BFF` (com poder) e em `public-api` (sem scope). Logo o **www BFF concede acesso amplo ao token de sessão** → é ali que BOLA mora, não no public-api. *Suposição do dev:* "o token de sessão só chega no BFF" — mas ele é aceito no developer API também.
2. **Gate de onboarding (Q4):** TODO endpoint checa "onboarding completo"? Ou endpoints per-user (balances/feeds) servem dado a um token cuja conta está **em verificação de telefone**? Se algum serve → step-skip + acesso pré-verificação.
3. **Delegação agent→user-token (Q1+Q2):** `POST /v2/agent-portfolios/{agentPortfolioId}/user-tokens` — o backend valida (a) que sou dono do `{agentPortfolioId}`, (b) que os `scopeNames` estão no meu grant, (c) consentimento do usuário-alvo? Furou qualquer um → fundos alheios.
4. **Anti-abuso do telefone (Q6 fail-open):** verificação SMS rate-limited (1015). O provedor SMS indisponível → ramo marca `verified`? `client_request_id` é client-controlled. 
5. **CF/DataDome (Q3 caminho alternativo):** os hosts quentes (webhooks, bo, argocd, consul) são 403-CF. Existe **origin** atrás do CF (IP direto) que pula o WAF?

## Mapa do não-testado (ranqueado por fragilidade do modelo)
1. **`www.etoro.com/api/*` reads com o token mid-onboarding** — nunca sondado; o teste mais barato que destrava (fetch no console do browser). Responde a costura #2.
2. **Onboarding step-skip / phone bypass** — pular `initiate-phone-verification` e chamar a etapa seguinte direto (Q4); fail-open do SMS (Q6). *(endpoint rate-limited ~23h agora)*
3. **`mcp.public-api.etoro.com` (200, vivo)** — enumerar tools do MCP, auth, prompt-injection. Superfície nova, 0 saturação.
4. **Delegação agent-portfolio** (E6/E7) — precisa conta onboarded.
5. **Webhooks de custódia** (origin behind CF) — forjar confirmação de depósito (N3).
6. **`etorologsapi /api/v2/monitoring`** — reflete dados do cliente (log/beacon injection, arsenal S-BEACON).

## Cadeias narrativas (multi-passo — impacto se o modelo estiver certo)
- **N1 — bypass de onboarding → acesso completo** *(melhor agora)*: pular verificação de telefone (step-skip #2 ou fail-open SMS) → alcançar o app → gerar API key OU usar o www BFF → testar tudo. **Se funcionar pra qualquer conta = achado sozinho** (registro sem verificar identidade num broker regulado).
- **N2 — delegação → controle de fundos**: token de sessão amplo no www BFF + `create/patch user-token` em `agentPortfolioId` alheio com scope elevado → agir sobre fundos de terceiro. Elos: costura #1 + #3.
- **N3 — webhook de custódia forjado**: achar origin de `*fireblockswebhook/bitgowebhook/fiatproviderwebhook` atrás do CF → POST forjado `status:success` sem assinatura → crédito sem pagar.
- **N4 — MCP**: `mcp.public-api` vivo → enumerar tools → tool com ação privilegiada / injeção.
- **N5 — cross-surface privesc**: se um endpoint do public-api **não** checar scope (diferencial vs os 5 que deram 403) → o token de sessão web o alcança sem a API key.

**Próxima a executar:** **N1 via não-testado #1** — testar os reads do `www.etoro.com/api/*` com o token mid-onboarding (fetch no console do browser; não toca o endpoint rate-limited; responde "o gate de onboarding tem furo?"). Barato, destrava, e pode ser achado por si só.

---

# `armar a arapuca` — busca offline Wayback+openapi (2026-09-05, escudo 🛡️)

Objetivo: achar os **elos vazados** (ids enumeráveis/públicos) que faltavam pras chains C1/C2/C4. Fonte: `recon/urls.txt` (6000 Wayback) + `recon/apidocs/openapi.json` (142 paths).

## Elos ENCONTRADOS
- **✅ `postId` é SEQUENCIAL** — Wayback expõe `PostID=` inteiros **113446 → 152151** (31 vistos, pareados com `action=copytrader`). **Fecha o elo de C-social:** subárvore `{postId}` do openapi é enorme (`GET/PUT/DELETE /api/v1/posts/{postId}` + `/comments`,`/replies`,`/likes`,`/polls/{pollId}/options/{optionId}/votes`,`/pins`,`/follows`,`/saves`). E13/E15 agora testáveis c/ 1 conta via probe não-destrutivo (marker id, 401-vs-404). **P3 (id previsível = IDOR até prova).**
- **✅ `AffiliateID` sequencial** — inteiros 10869→19134 no Wayback (etoropartners). Elo de BOLA pro back-office de afiliados.
- **`gcid` próprio conhecido** (49462268) → `GET /api/v1/impressions/topassets/user/{gcid}` é BOLA-shaped (mas Public API = scope-wall).

## Elos DESCARTADOS (economia de tempo)
- **❌ `client_request_id`** (181 UUIDs vazados, pareados c/ `action=get_pi_list`): confirmado no openapi que é o header **`x-request-id`** (tracing/idempotência), **NÃO** referência de recurso. **Peça morta** — não é BOLA.
- **❌ accountId/positionId/orderId/clientId/transferId/watchlistId**: ZERO vazados no Wayback. Vivem atrás de auth (BFF). C1/C2/C4 seguem sem elo até capturar cURL logado.

## Ramo `partners/admin` — DORMANT (fechado)
Wayback expôs (via param `GoToAfterLogin=`) o mapa do back-office .NET `www.etoro.com/partners/admin/*.aspx` — funções de $$$ (`Tools_PayAffiliates`, `Tools_ApprovePayment`, `CreditNote`, `Reports_PaymentHistory/AffiliateList`, `FileUpload`). Probe (GET único, header `X-Bug-Bounty`, você rodou):
1. `www.etoro.com/partners/admin/login.aspx` → **301 (Cloudflare)** → `partners.etoro.com/admin/login.aspx`. Cookie `Domain=etoro.com`.
2. `partners.etoro.com/admin/login.aspx` → **301 (AkamaiGHost)** → `por.etoro.com/`.
3. `por.etoro.com/` → **200, SPA Angular "eToro Partners", `Server: AmazonS3`**.
→ **Veredito:** o admin .aspx do Wayback está **DECOMISSIONADO** (2× 301 pra fora). Mapa histórico, superfície morta. **`[DORMANT]` — não retestar as .aspx.**

## Ramo NOVO reaberto — `por.etoro.com` (partner portal SPA)
O admin morreu mas virou SPA moderna. Superfície fresca:
- **`Server: AmazonS3` + `x-amz-*`** → estáticos direto de bucket S3 → candidato a **bucket misconfig/listing** (P7, in-scope).
- **`git-version: portal-version-67-ga136b166`** → disclosure de versão; JS bundle expõe o mapa da **partner-API** (`partners-api.*` resolve) → BOLA de dado de afiliado/pagamento (o prêmio, na versão viva).
- **[NEXT · UNTESTED]** extrair `main.*.js` → grep offline por base da partner-API, rotas `/api/`, hosts, chaves. Depois: probe não-destrutivo de BOLA na partner-API + check de listing do bucket S3.
