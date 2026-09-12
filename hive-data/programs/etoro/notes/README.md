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
# eToro — ficha de alvo

- **Programa:** eToro Managed BBP · **Bugcrowd** · fintech / social trading (multi-asset) · Safe Harbor · **triagem expedita**
- **Link:** Bugcrowd (eToro Managed Bug Bounty Engagement) · docs: https://help.etoro.com
- **Prioridade:** 🟢 **ALTA ⭐** — top pick pro 1º bounty (wildcard fintech + triagem rápida + paga lows)
- **Escopo (3/4, amplo):**
  - **`*.etoro.com`** (wildcard — 37 known issues, main já garimpado)
  - delta.app (ReactJS) · etorox.com (.NET/IIS) · etoropartners.com (.NET/IIS)
  - Mobile: com.etoro.wallet · com.etoro.openbook · io.getdelta (iOS/Android)
- **Reward:** P1 $6k–15k · P2 $1.5k–6k · P3 $500–1000 · P4 $100–500. Avg $618/3mo (⇒ **paga muitos lows** = 1º bounty acessível).
- **Stats:** 66 vulns premiadas · **validação em 2 dias** (75% decididos em 2d) · triagem expedita Bugcrowd.

## ⛓️ Constraints pré-fixadas
- Conta self-provisioned com e-mail `@bugcrowdninja.com` (pode registrar várias).
- **Header obrigatório:** `X-Bug-Bounty:<bugcrowdusername>` em todo tráfego HTTP.
- Registrar antes de testar: IP, user-agent, usernames usados (podem pedir).
- **🚫 Sem tooling de volume / stress / DoS / rate-limit bypass / email bombing.** Upload: máx ~75 arquivos.
- **🚫 Não tocar em contas de terceiros** sem consentimento escrito; não acessar dado de usuário/empresa.
- N-day: só in-scope após 30 dias do patch público.
- Fora de escopo notável: Clickjacking/CORS (usam Cordova), Facebook SDK mobile, enumeração, self-XSS, rate-limit.
- **OOS com bônus discricionário:** ativos da eToro fora da lista podem ser aceitos se impacto claro (reward ~50% menor + bônus se alto impacto).

## 🎯 Onde mirar (fit BAC/lógica) — INSIGHT PRINCIPAL
- **O main `etoro.com` já tem 37 known issues → está garimpado.** Mire os **ativos secundários** com 0–2 known issues:
  - **delta.app (ReactJS SPA)** → client-side + API = IDOR/BAC com menos gente olhando.
  - **etorox.com / com.etoro.wallet / openbook** → superfícies mais novas.
- Lógica de fintech social = ouro pro seu perfil:
  - **Copy-trading** (copiar carteira de outro): quem autoriza o quê? acesso a portfólio/posições alheias?
  - **Carteira / saldo / ordens**: IDOR de posição, manipulação de valor/fee, lógica de execução.
  - **Social features** (feed, perfis, seguir): acesso não-autorizado a dado de outro trader.
- Rodar as 6 perguntas do The Mind em cada endpoint de API.

## Recon
- `code 0` no wildcard `*.etoro.com` pra enumerar subdomínios (aqui o recon rende — wildcard grande). Depois focar manual nos secundários.
- Sempre com o header `X-Bug-Bounty`. Sem scanners de volume.

---

## 🏦 Recon massivo — `protocolo ladrão de bancos` (2026-09-05, fase PASSIVA)
Fontes: subfinder (passivo) + crt.sh nos 4 domínios in-scope. **574 subdomínios únicos** (etoro.com/etorox.com/etoropartners.com/delta.app). Distribuição: `.int.` 40 · `.stg.` 34 · `.dev.` 43 · `.preprod.` 12 · prod/outros ~445. Cru em `recon/subdomains.txt`.
⚠️ **NADA foi probado ainda** — todo host abaixo está `[não-probado]`; a fase ativa (httpx/katana) precisa do header `X-Bug-Bounty:<username>` (pendente). `gau` falhou (0 urls); waybackurls retentando.

### Clusters de alto valor (fintech) → mapeados aos sinais do `memory/signals.md`
| Cluster | Hosts-chave | Sinal | Por que |
|---|---|---|---|
| 💳 **Webhooks de custódia** | `bitgowebhook.wallet`, `prod-cfireblockswebhook.wallet`, `simplex.wallet`, `billing-pci`, `int/stg-bitgowebhook` | **S-WH-01 / S-PAY-01** | webhook de BitGo/Fireblocks/Simplex sem verificação de assinatura → forjar confirmação de depósito/transfer = $$$. **Topo.** |
| 🪙 **Wallet / signing** | `sign.goodwallet`, `iam.sign.goodwallet`, `relay.sign.goodwallet`, `wallet.etoro.com`, `money-front-public.wallet` | S-IDOR-01 / S-WH-01 | infra de assinatura de tx cripto; IDOR de saldo/posição; relay abusável |
| 🔐 **OAuth sprawl** | `oauth-back/front/ob.wallet`, `oauthob`, `oboauth`, `stg-walletoauth.dev`, `login.api`, `auth.stg` | **S-OAUTH-01 / S-REDIR-01** | 6+ hosts OAuth no wallet → `redirect_uri` frouxo/token leak = ATO (ver técnica nova no arsenal) |
| 🚪 **Gateways / BFF** | `apigw-bff.dev/int`, `apigw.dev/int`, `public-api.int/stg`, `gateway.cloud` | **S-IDOR-02 / S-HDR-01** | BFF é ninho de BOLA/IDOR; gateway pode não stripar headers internos de confiança |
| 🛠️ **Back-office / admin** | `bo.prod/int/stg`, `admin.connect`, `mail.bo.int` | **S-BAC-01 / P7** | painel admin/BO force-browsável; authz por rota |
| 🤖 **MCP server** | `mcp.public-api.etoro.com`, `mcp.public-api.stg` | (novo — LLM) | servidor MCP exposto → prompt injection, abuso de tool, auth do MCP |
| ⚙️ **Infra CI/monitor** | `jenkins.candle`, `jenkins.go`, `grafana.connect` | S-INFO-01 / P7 | Jenkins/Grafana no DNS público → se unauth = RCE/dashboard interno |

### Hipóteses [UNTESTED] priorizadas (fit BAC/lógica + fintech)
1. **[UNTESTED · S-WH-01] Webhook de custódia sem verificação de assinatura** — `bitgowebhook`/`cfireblockswebhook`/`simplex.wallet`: POST forjado com `status:success`/payload de depósito → crédito sem pagar. **Maior EV.** Probe (ativo): mapear o path do webhook, testar payload sem assinatura/replay. ⚠️ pare-e-confirme (mutante).
2. **[UNTESTED · S-OAUTH-01/S-REDIR-01] `redirect_uri` no OAuth do wallet** — os 6+ hosts oauth: allowlist frouxa → roubo de code/token = ATO. Probe: puxar `/.well-known/*`, variar `redirect_uri` (unauth, lê o erro).
3. **[UNTESTED · S-IDOR-02] BOLA no `apigw-bff` / `public-api`** — BFF concentra chamadas por-usuário; `userId/accountId` no body → dado alheio. **Bate no seu ponto forte.** Precisa 2 contas `@bugcrowdninja.com`.
4. **[UNTESTED · S-BAC-01] Back-office force-browse** — `bo.*`, `admin.connect`: authz só na UI? chamar rota interna direto.
5. **[UNTESTED · lógica copy-trading]** (do insight da ficha) — copiar carteira: acesso a portfólio/posições alheias; manipular fee/valor.
6. **[UNTESTED · MCP] `mcp.public-api`** — enumerar tools do MCP, testar auth e injeção.
7. **[UNTESTED · P7] Jenkins/Grafana** — checar se `jenkins.*`/`grafana.connect` respondem unauth.

8. **[UNTESTED · S-REDIR-01] Open redirect** — Wayback (6000 urls, `recon/urls.txt`) mostra params `TargetURL=` (9×), `url=` (28×), `redirect=` no `etoro.com`. Testar allowlist (`//evil`, `@`, etc.). ⚠️ eToro lista open-redirect isolado como low; vale por chain (OAuth do wallet).
9. **[UNTESTED · lógica/KYC] ações sensíveis via query** — `?action=autokyc&deepLink=uploadutilitybill`, `?action=copytrader`, `?action=get_pi_list&client_request_id=<uuid>`. O `autokyc` (KYC deep-link) e `copytrader` cheiram a lógica abusável; `client_request_id` = candidato a IDOR.

**Próximo passo:** fase ativa (probe httpx dos ~574 com `X-Bug-Bounty`) pra ver quais estão vivos e o que servem → afunilar os 9 leads. ✅ **FEITO abaixo.**

---

## ⚔️ Fase ATIVA — probe httpx (2026-09-05, header `X-Bug-Bounty: moldret`)
`dnsx` (574→430 resolvem) → httpx fast-fail. **412 hosts respondendo.** Cru em `recon/probe.jsonl` + `resolved.txt`.
> **⚡ Nota:** o probe pendurou 30 min na 1ª tentativa (hosts internos que resolvem mas não respondem). Otimização (dnsx pré-filtro + `-timeout 5 -retries 1` + pipe/stdout) → 1 min. Registrada no `protocolo ladrão de bancos` § passo 4.

### 🔑 INSIGHT que muda a estratégia: Cloudflare-gating
Os hosts **mais quentes** (webhooks de custódia, `bo.*`, `apigw`, `argocd`, `consul`, `public-api-int/stg`) respondem **403 "Attention Required! | Cloudflare"** — é **WAF do Cloudflare bloqueando o probe automatizado**, NÃO auth de app. Um **navegador real** (que passa o challenge CF) provavelmente alcança. → **território da `formação tridente`** (dirigir navegador real por eles) OU achar o **origin** atrás do CF.

### 🟢 Vivos e DIRETAMENTE atacáveis (200 — sem CF-gate)
| Host | Serve | Lead |
|---|---|---|
| **`mcp.public-api.etoro.com`** · `agent.public-api` · `mcp-test.delta.app` | **"eToro Public API MCP" / "Delta MCP"** — servidores **MCP vivos** | 🔥 superfície LLM/tool nova, pouquíssimo olhada: enumerar tools, auth do MCP, injeção |
| **`oauth.wallet.etoro.com`** | **"OpenBanking"** (OAuth vivo) | 🔥 `redirect_uri`/token → ATO (S-OAUTH-01/S-REDIR-01) |
| **`api-portal.etoro.com`** · `builders.etoro.com` | **eToro API Docs / Builders Portal** | 🗺️ **mapa da API** → alvos de IDOR/BOLA (teu ponto forte) |
| **`goodwallet.etoro.com`** (Vercel) · `kyc.etoro.com` | wallet app · KYC (casa com `?action=autokyc` do wayback) | IDOR de saldo/posição; lógica de KYC |
| **`cashier.etoro.com`** (→`/cashiermvc/`) · `billing-pci` (→`/Error`) | **cashier .NET MVC** · billing PCI | lógica de pagamento; MVC = endpoints previsíveis |
| `push-*.cloud.etoro.com` | **Lightstreamer 7.4.0** (real-time push) | version-specific; auth do stream/subscription |

### 🔒 Quentes mas CF-gated (403 challenge — precisam de navegador/origin)
`{bitgo,int-bitgo,stg-bitgo,stg-fiatprovider}webhook.*` (webhooks custódia) · `bo.{prod,int,stg}` (back-office) · `apigw(-bff).{dev,int}` · **`argocd-{ne,we}.prod`** (ArgoCD — GitOps!) · **`consul-{ne,we}.prod`** (Consul service-mesh!) · `public-api.{int,stg}`.
→ `argocd`/`consul` unauth = controle de cluster (Critical-shape) **se** o CF-gate for contornável ou o origin exposto.

### Leads refinados (pós-probe)
1. **[UNTESTED] MCP servers vivos** (`mcp.public-api`, `agent.public-api`, `mcp-test.delta.app`) — enumerar tools/auth; superfície novíssima. **Topo (baixa saturação).**
2. **[UNTESTED] `oauth.wallet` OpenBanking** — `redirect_uri`/token (usar a técnica Azure-AD adaptada no `arsenal.md`).
3. **[UNTESTED] API map via `api-portal`/`builders`** — ler os docs → derivar endpoints de IDOR/BOLA.
4. **[UNTESTED · tridente/origin] cluster CF-gated** — webhooks + `argocd`/`consul` + `bo.*`: passar o CF (navegador real = tridente) ou achar origin.
5. **[UNTESTED] `cashier` .NET MVC** — enumerar `/cashiermvc/*`, lógica de pagamento.
