---
program_id: etoro
document_type: note
classification: internal
claimed_scope_status: unknown
source: excalibull/targets/etoro/context.txt
collected_at: 2026-09-08T16:41:49Z
tags: [excalibull, import, estado-investigacao, sintese]
asset_refs: [etoro.com, www.etoro.com, public-api.etoro.com, mcp.public-api.etoro.com, agent.public-api.etoro.com, por.etoro.com, affapi.etoro.com, oauth.wallet.etoro.com, cashier.etoro.com, kyc.etoro.com, goodwallet.etoro.com, etorologsapi.etoro.com]
---

CONTEXT.TXT — eToro, síntese da caçada até agora (2026-09-05, postura escudo 🛡️)
=================================================================================
Companion de `arapuca.md` (peças/chains) e `README.md` (ficha do alvo). Este arquivo
é o resumo de ONDE ESTAMOS, pra retomar sem reler tudo.

---------------------------------------------------------------------------------
1) POSTURA E REGRAS ATIVAS
---------------------------------------------------------------------------------
- `postura escudo` ligada nesta sessão inteira: só o que NÃO dispara flag (WAF/rate-limit/
  blue team não distingue de tráfego normal). Lança (ataque ativo) fica desligada.
- Toda chamada AUTENTICADA é o Tiago quem roda (regra de delegação) — Claude desenha o
  comando + interpreta o resultado, nunca executa com a própria rede.
- GETs anônimos/públicos de baixo volume, seriais, offline parsing de JS/openapi/Wayback:
  Claude pode fazer/pedir direto, sem passo extra.
- Header obrigatório em todo tráfego do engagement: `X-Bug-Bounty: moldret`.
- Escopo: `*.etoro.com` (wildcard) + `etorox.com` + `etoropartners.com` + `delta.app`.
- Proibido: scan de volume, fuzz, brute, tocar conta de terceiro, payload de injeção,
  qualquer coisa que pareça abuso automatizado.

---------------------------------------------------------------------------------
2) ESTADO DA CONTA DE TESTE (o muro real)
---------------------------------------------------------------------------------
- Conta REAL (não-demo): <REDIGIDO:email-conta-teste> / user moldretvalor7284 /
  gcid 49462268 / cid 50640014.
- BLOQUEADA no onboarding: verificação de telefone rate-limited (Cloudflare erro 1015,
  janela ~23h a partir de 2026-09-05 00h42 -03). Sem isso → sem KYC → sem API Key formal
  → `Credit: 0` → app gated.
- SCOPE WALL CONFIRMADO (6/6 testes): o bearer de sessão WEB autentica em `public-api.
  etoro.com` e em AMBAS as instâncias MCP (`mcp.public-api`, `agent.public-api`), mas
  SEM NENHUM SCOPE — sempre `403 InsufficientPermissions`, nunca `401`. MCP é proxy
  honesto (authChannel:"bearer", relay puro pro mesmo backend) — sem bypass ali.
  => A Public API / MCP estão MORTAS enquanto o telefone não destravar. NÃO insistir
     nesse caminho até a janela de rate-limit passar (~2026-09-06 ~00h -03).
- CAMINHO VIVO, sem depender da conta destravada: o MESMO bearer de sessão web tem
  PODER TOTAL no BFF `www.etoro.com/api/*` — contexto de conta completo, só checa
  sessão, não scope. É ONDE A BOLA DE VERDADE MORA. Testável com 1 conta só (Q1 sem
  2ª conta: id que não é meu → 200/sucesso vs 403/erro).

---------------------------------------------------------------------------------
3) MODELO DO SISTEMA (visão geral, camadas)
---------------------------------------------------------------------------------
  Cloudflare (edge, WAF, cf_clearance, rate-limit 1015) + DataDome (bot mgmt)
        |
  www.etoro.com/api/*  (BFF do app web — AQUI a BOLA vive)
        |-> bearer JWE (kid 04434, dir A256CBC-HS512, claim AccountType)
        |-> headers: X-Session-Id, X-STS-DeviceId, ApplicationIdentifier
        |
  public-api.etoro.com (developer API, 142 paths, openapi público) --- SCOPE WALL
  mcp.public-api.etoro.com / agent.public-api.etoro.com (MCP, JSON-RPC) --- SCOPE WALL
  backend services: wallet/custódia (BitGo/Fireblocks/Simplex webhooks), oauth.wallet,
  cashier (.NET MVC), kyc.etoro.com, goodwallet.etoro.com (Next/Vercel)

  NOVO (achado nesta sessão): por.etoro.com — "eToro Partners" portal, SPA Angular,
  estáticos em S3, backend próprio `affapi.etoro.com` + auth via Azure AD B2C. Ver §6.

---------------------------------------------------------------------------------
4) PEÇAS MAPEADAS NA arapuca.md (resumo — ver arquivo pra detalhe/tabelas)
---------------------------------------------------------------------------------
Fonte: openapi.json v1.367.0 (142 paths, público em api-portal.etoro.com).
Todas [UNTESTED] contra o BFF real ainda — mapeadas via Public API mas o mecanismo
de owner-check é o mesmo backend, então testável via BFF assim que houver cURL logado.

  DINHEIRO (crown jewels):
    E1  GET /api/v1/balances/{accountType}/{accountId} (+/history) — ler saldo alheio
    E2  GET /api/v1/money/accounts/cash/{accountId}/transactions   — extrato alheio
    E3  GET /api/v1/money/transfers/{transferId}                   — transferência alheia

  SSO/OAuth app (maior impacto isolado):
    E4  POST /api/v1/sso/applications/{clientId}/client-secret — regenerar secret de
        app OAuth ALHEIO → impersonar app → ATO em massa dos usuários dele. clientId
        É PÚBLICO no fluxo OAuth (elo já disponível). MELHOR CHAIN CANDIDATA (C2).
    E5  PUT/DELETE /api/v1/sso/applications/{clientId} — editar/deletar app de terceiro

  Token de agente (mais profundo, privesc de scope):
    E6  POST /api/v2/agent-portfolios/{agentPortfolioId}/user-tokens (+scopeNames) —
        criar token em portfolio alheio ou pedir scope elevado (trade/withdraw)
    E7  PATCH .../user-tokens/{userTokenId} — escalar scope de token existente
    E8  DELETE .../user-tokens/{userTokenId} — revogar token de terceiro (DoS)

  Trading (dano real):
    E9  POST /api/v1/trading/execution/market-close-orders/positions/{positionId} —
        fechar posição REAL de outro usuário
    E10 PATCH /api/v2/trading/positions/{positionId} — alterar SL/TP alheio
    E11 DELETE /api/v2|v3/trading/execution/orders/{orderId} — cancelar ordem de terceiro
    E12 par demo/real em todo endpoint de execução — testar token demo no path real

  Social (menor valor mas com ELO JÁ CONFIRMADO — ver §5):
    E13 PUT/DELETE /api/v1/posts/{postId} — editar/deletar post alheio
    E14 PUT/DELETE /api/v1/watchlists/{watchlistId} (+/rank, /setUserSelectedUserDefault)
    E15 .../posts/{postId}/comments/{commentId} PUT/DELETE, /polls/.../votes

  MCP disclosure (Low/Info, sem bypass demonstrado):
    E17 mcp.public-api expõe 191 rotas vs 172 documentadas no portal — 12 rotas de
        trade "escondidas" testadas anônimo → 401 igual às documentadas. Sem furo.
        Técnica p/ arsenal: MCP sem credencial = oráculo de contagem de rotas.

  MORTO (contra-prova já rodada):
    E16 header AccountType no BFF — FALSO POSITIVO confirmado (é o toggle Demo/Real
        legítimo da UI, não bypass cross-user). NÃO RETESTAR.

  Chains: C1(E1+E2, exfil massa) · C2(E4, OAuth takeover — MELHOR AGORA) ·
          C3(E6/E7, sequestro de fundos via agente) · C4(E9/E10/E11, dano em posição) ·
          C5(E12, demo→real)

---------------------------------------------------------------------------------
5) BUSCA OFFLINE (Wayback 6000 urls + openapi) — ELOS que faltavam
---------------------------------------------------------------------------------
ENCONTRADOS:
  - postId É SEQUENCIAL — Wayback mostra PostID= inteiros 113446→152151 (31 vistos,
    pareados com action=copytrader). FECHA O ELO de E13/E15 (BOLA social). Testável
    com 1 conta via probe não-destrutivo (marker id, diferencial 401 vs 404).
  - AffiliateID sequencial — 10869→19134 no Wayback (etoropartners). Elo pro
    back-office de afiliados (ver §6, hoje morto mas pode reaparecer no portal novo).
  - gcid próprio conhecido (49462268) → GET /api/v1/impressions/topassets/user/{gcid}
    é BOLA-shaped, mas vive na Public API (scope wall) — baixa prioridade por ora.

DESCARTADOS (não insistir):
  - client_request_id (181 UUIDs vazados) = confirmado no openapi que é o HEADER
    x-request-id (tracing/idempotência), NÃO referência de recurso. Peça morta.
  - accountId / positionId / orderId / clientId / transferId / watchlistId: ZERO
    vazados no Wayback. Vivem atrás de auth no BFF — só aparecem com cURL logado.

---------------------------------------------------------------------------------
6) RAMO `partners/admin` (.NET/.aspx) — MORTO. RAMO NOVO `por.etoro.com` — VIVO E QUENTE
---------------------------------------------------------------------------------
Sequência de probes (GETs únicos, você rodou, header X-Bug-Bounty):

  www.etoro.com/partners/admin/login.aspx
    -> 301 (Cloudflare) -> partners.etoro.com/admin/login.aspx
    -> 301 (AkamaiGHost) -> por.etoro.com/
    -> 200 OK — SPA Angular "eToro Partners", Server: AmazonS3

  VEREDITO: o back-office .aspx do Wayback (Tools_PayAffiliates, Tools_ApprovePayment,
  CreditNote, Reports_PaymentHistory/AffiliateList, FileUpload — vazados via param
  GoToAfterLogin=) está DECOMISSIONADO (2x 301 pra fora). [DORMANT] — não retestar
  as rotas .aspx específicas.

  MAS o portal de afiliado MIGROU pra uma stack moderna — e essa é viva:

  por.etoro.com (SPA Angular, "eToro Partners", git-version: portal-version-67-ga136b166)
  Bundles JS baixados e analisados OFFLINE (recon/por-js/):
    chunk-AVJQPSXF.js, chunk-EVBZJL4M.js, chunk-FJE3VJJW.js, chunk-JD2CSVPC.js,
    polyfills-6ISPNSXF.js, main-MEAMHKX3.js

  CONFIG DE AMBIENTE extraída de chunk-FJE3VJJW.js (objeto `environment`, v2.2.5):
    API base:        https://affapi.etoro.com/api/        <- backend do portal de afiliado
    logger API:       https://etorologsapi.etoro.com/api/v2/monitoring
    loggerIdentifier: "partners-portal"

    Azure AD B2C (auth do portal):
      clientID:        9c7105c0-57de-4677-95b4-f6be40d25f8a
      authority:       https://etoropartners.b2clogin.com/tfp/etoropartners.onmicrosoft.com/B2C_1_signin
      authoritySignup: https://etoropartners.b2clogin.com/tfp/etoropartners.onmicrosoft.com/B2C_1_signup
      b2cScopes:       ["https://etoropartners.onmicrosoft.com/partnersapi/user_impersonation"]
      redirectUri:         https://por.etoro.com/en-gb/dashboard
      postLogoutURI:       https://por.etoro.com/en-gb/auth/login
      postSignupLogoutURI: https://por.etoro.com/en-gb/auth/registration

    Chaves client-side expostas no bundle (nível de exposição normal p/ SPA, mas
    registrar — candidatas a checar restrição/least-privilege):
      mixpanel.token:  <REDIGIDO:mixpanel-token>
      mixpanel.apiKey: <REDIGIDO:mixpanel-apikey>
      googlePlacesApiKey: <REDIGIDO:google-places-key>

  Rotas internas achadas até agora (grep raso, incompleto — router provavelmente
  lazy-loaded em chunk não capturado por esta busca):
    Angular router paths vistos: "auth", "dashboard" (poucos — via lazy loading)
    affapi endpoint concreto visto: "v1/countries" (auxiliar, baixo valor)

  LEADS ABERTOS NESTE RAMO (nenhum testado ainda — tudo cabe no escudo por ora):
    a) Server: AmazonS3 nos estáticos -> checar se o BUCKET (não só o CDN/domínio)
       está com listing público ou é acessível fora do CloudFront/domínio wrapper
       (P7). Precisa achar o nome/URL real do bucket (não visto ainda nos headers
       capturados — só "Server: AmazonS3", sem x-amz-bucket-region ou host S3 direto).
    b) affapi.etoro.com é OAuth2/B2C real (fluxo `oauth2/v2.0/authorize` visto no
       bundle) — MESMO PADRÃO da técnica Azure-AD já registrada no arsenal (testar
       `redirect_uri` frouxo / tenant confusion / cross-tenant no B2C_1_signin).
    c) Rotas de negócio da affapi (pagamento, comissão, relatório de afiliado — o
       que as .aspx antigas faziam) ainda não mapeadas — os chunks principais
       (dashboard/rotas lazy) não foram capturados por este primeiro GET da SPA;
       precisa ou (i) baixar mais lazy chunks referenciados dinamicamente, ou
       (ii) navegar a SPA de verdade (login B2C) pra ver as chamadas de rede —
       ambos exigem ação sua (login redireciona pra domínio Microsoft, fora do
       escudo tocar autenticado).
    d) `affapi.etoro.com/api/` — testar GET anônimo na raiz (silencioso, 1 request)
       pra ver se vaza algo (Swagger, mensagem de erro com stack, CORS) — AINDA
       NÃO FEITO.

---------------------------------------------------------------------------------
7) ACHADOS FINAIS — `por.etoro.com` (2026-09-06, escudo 🛡️)

Três **NOVAS PEÇAS** descobertas na superfície de afiliados:

  **E18 🔥 [UNTESTED]** — `GET /api/v1/ib/Traders/{id}` (BOLA de trader alheio)
    Path-param direto a objeto → dados de copier/portfolio de outro afiliado.
    P: P1 (PII + financeiro).
    Bloqueador: precisa Bearer B2C (signup UI complexa).

  **E19 [UNTESTED]** — `POST /v1/Affiliates/exists?email={email}` (enumeração)
    P: P4/OOS (fora de escopo — enumeração isolada não paga).
    Status: 401 Unauthorized, www-authenticate: Bearer (confirmado 2026-09-06).

  **E20 [UNTESTED]** — `POST /v1/Affiliates/register/{private,corporate}` (auth-gated)
    P: P2 (se step-skip for possível = lógica de negócio).
    Status: 401 Unauthorized (confirmado).
    Insight: Signup B2C contorna muro do telefone — IF conseguir registrar, ganha Bearer → E18.

Backend mapeado: `affapi.etoro.com` (9 serviços, v1/* endpoints).
B2C completo: tenant + policies + OAuth2 (fluxo implícito habilitado).

**Veredito:** Ramo VIVO, alto potencial (E18 = joia isolada), mas signup UI é barreira prática.
Candidato pra segunda sessão com melhor acesso ou contorno alternativo.

---

7b) PRÓXIMOS PASSOS RECOMENDADOS (ordem por EV / custo)
---------------------------------------------------------------------------------
  1. [offline/escudo, Claude pode fazer sozinho] GET anônimo único em
     `affapi.etoro.com/api/` e em `https://etoropartners.b2clogin.com/tfp/
     etoropartners.onmicrosoft.com/B2C_1_signin/.well-known/openid-configuration`
     — mapear o B2C sem tocar em nada sensível (leitura pública de metadata OIDC).
  2. [1 clique, você] Logar no por.etoro.com com a conta de afiliado (se tiver) OU
     simplesmente abrir a tela de signup do B2C pra ver o fluxo `redirect_uri` real
     — aplica a técnica Azure-AD do arsenal.
  3. [1 conta, probe não-destrutivo, você roda 1 cURL] Owner-check em E13/E15
     (postId sequencial confirmado) via BFF: capturar cURL de "editar meu próprio
     post", eu construo a variante com postId vizinho (não meu, não de terceiro
     real conhecido) e comparamos 403/401 vs 404-igual-a-inexistente.
  4. [aguarda ~23h] Quando o rate-limit do telefone passar: retomar onboarding,
     testar step-skip (pular verificação de telefone indo direto pra KYC/API key).
  5. [baixa prioridade por ora] `agent.public-api` vs `mcp.public-api` diferencial
     de credencial (não testado, mas Public API está scope-walled de qualquer forma).

---------------------------------------------------------------------------------
8) O QUE JÁ FOI TESTADO E FECHADO (não retestar)
---------------------------------------------------------------------------------
  - E16 (header AccountType) — FALSO POSITIVO, padrão legítimo da UI.
  - E17 (MCP rotas escondidas) — Low/Info, mesmo enforcement, sem bypass.
  - Scope wall Public API/MCP — 6/6 confirmado, sem furo, é bloqueio real de scope.
  - www.etoro.com/partners/admin/*.aspx — DORMANT, decomissionado (2x 301 pra fora).
  - client_request_id como candidato a IDOR — descartado, é só o header x-request-id.
