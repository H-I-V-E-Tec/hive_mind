---
program_id: yahoo
document_type: note
claimed_scope_status: unknown
classification: internal
source: intigriti policy (yahoo) — 2026-09-08
collected_at: 2026-09-12T00:00:00Z
tags: [recon, yahoo]
asset_refs: [apis.mail.yahoo.com]
---
# Fantasy Wallet / Sports — superfície de money/IDOR (2026-09-08)

## Auth model
- `api-rw-secure.fantasysports.yahoo.com` → **403 `RBAC: access denied`** unauth (gateway estilo Envoy/Istio RBAC). É a **Fantasy API v2 (RW segura)**.
- `checkout.fantasysports.yahoo.com/` → 307 → `login.yahoo.com/?.done=https://subscriptions.payments.yahoo.com/` (auth-gated → payments).
- Auth = **sessão Yahoo por cookie** (`.done=` flow). Conta `moldret_intigriti` autentica (sem aprovação de creator — diferente do CMS).
- `wallet.secure.yahoo.com`, `account.api-rw-secure.*`, `api-rw.fantasysports` = NXDOMAIN/timeout (mortos/internos de fora).

## Endpoints (CDX passivo)
### api-rw-secure.fantasysports.yahoo.com — Fantasy API v2 (gramática YQL matrix `;`)
`/fantasy/v2/users;use_login=1/games;game_keys={nfl,nba,mlb}/leagues;out=settings/players;player_keys=<game>.p.<id>;out=ownership,roster_actions`
- Recursos por **key**: `users`, `games`, `leagues` (`nfl.l.<id>`), `teams` (`nfl.t.<id>`), `players` (`nfl.p.<id>`), `transactions`.
- **BOLA:** ler/editar league/team de outro usuário por key (keys ~sequenciais/enumeráveis). **Write:** `roster_actions`, add/drop, trades, transações.

### checkout.fantasysports.yahoo.com (entry-fee/carrinho)
`/checkout/v1` · `/api/v1/checkout/cart/product/bundled` · `/checkout/referral` · `/checkout/v1/confirmation` · `/api/v1/content/cms/omp/omp_checkout_yahoo_fantasysports_en_US`
- **Lógica de $:** preço/qty do produto no cart; IDOR de order/confirmation.

### subscriptions.payments.yahoo.com (hub de pagamento)
`/checkout/v1` · `/mysubscriptions` · `/subscriptions/plans` · `/auth/api` · `/api/privacyLinks`

## Próximo passo (precisa do Tiago — mínimo e limpo)
Capturar **1 request autenticada como cURL** (DevTools Network → Copy as cURL) de `api-rw-secure.fantasysports.yahoo.com` (ou `checkout`) logado — revela o auth exato (cookie/crumb/header) + shape p/ mutar em IDOR.
