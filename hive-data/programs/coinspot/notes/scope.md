---
program_id: coinspot
document_type: rules
claimed_scope_status: unknown
classification: internal
source: hackerone policy (coinspot) — CSV export 2026-09-09
collected_at: 2026-09-12T00:00:00Z
tags: [coinspot, root]
asset_refs: [www.coinspot.com.au]
---
# Escopo — CoinSpot (HackerOne · `coinspot`)

Fonte: https://hackerone.com/coinspot/policy (programa privado) · Última conferência: **2026-09-09** (export oficial de scopes do H1, arquivo CSV datado 2026-09-09 10:27 UTC).
> Reconferido contra a policy viva em 2026-09-09 via CSV de scopes exportado do H1. Sem mudança material vs. 2026-09-02.

> ⚠️ **Nota ao parser (`load_scope_md`):** a seção "Out of scope" é sticky — todo token entre crases parecido com host/rota depois dela vira regra OUT_OF_SCOPE. Por isso, abaixo da seção Out of scope **nada de assets entre crases** a não ser hosts realmente OOS.

## In scope

| Asset | Tipo (H1) | max_severity | Observações |
|-------|-----------|--------------|-------------|
| `www.coinspot.com.au` | URL | critical | host primário da app + API autenticada (`/api/v2`, `/api/v2/ro`, `/pubapi/v2`) |
| `www.coinspot.com.au/v2/api` | URL | critical | doc/endpoint da API v2 explicitado como asset próprio (add 2025-04-10) |
| `com.coinspot.app` | GOOGLE_PLAY_APP_ID | high | Android |
| `com.coinspot.app.ios` | APPLE_STORE_APP_ID | high | iOS · Apple Store ID 1541949985 · bundle 5Q5H52GDRT.com.coinspot.app |

Closed scope — **4 assets**, sem wildcard. Todos `eligible_for_bounty` + `eligible_for_submission`.
Campo `instruction` vazio em todos os assets do CSV → nenhuma instrução especial por-asset.
O apex sem `www` **não** está listado como asset no CSV vivo — só redireciona pra `www`; tratar API/app sempre pelo host `www`.

## Out of scope

| Asset / classe | Observações |
|----------------|-------------|
| `coinspot.com` | parking/redirect — H1 só cobre o TLD .com.au |
| `coinspot.co` | idem |
| `coinspot.io` | idem |
| `o2.comms.coinspot.com.au` | infra de e-mail (o3.ptr1105 / o4.ptr7513 idem) |

Classes excluídas (não são hosts — não entram como regra do parser):

- DoS / brute force — proibido pela policy.
- Rate-limit / brute em endpoint **não-autenticado** — OOS (em endpoint de auth não é auto-OOS; só brute distribuído contra senha/TOTP é excluído de High).
- Brute na cadeia de account-recovery — **proibido**; só teste lógico com conta própria.
- Reports de credenciais vazadas publicamente — OOS por si só (rebaixa a chain C3).
- "Enable 2FA without email confirmation" — known issue, pular.
- Upload malicioso via S3 pre-signed URL sem impacto demonstrável — OOS (mas implica que há pre-signed URLs — anotar como superfície).
- Libs vulneráveis sem PoC · browsers desatualizados · 0-day com patch < 1 mês — OOS.

## Regras relevantes

- **Testes automatizados:** permitido; **~5 req/s** (web) / **1000 req/min** (API v2). Sem fan-out agressivo.
- **Header de identificação:** o CSV de scopes não carrega texto de policy. Tiago tem acesso à policy viva (2026-09-09) — **pendente**: conferir no corpo da policy se exige header/tag (X-Bug-Bounty + handle) nas requests de teste. Até confirmar, assumir que NÃO exige; registrar aqui se aparecer.
- **Contas de teste:** 2 grátis; a registrada com `${HACKERONE_EMAIL_ALIAS}` (handle@wearehackerone.com) recebe **acesso de teste completo sem KYC**. API key gerada na página de API keys da conta.
- **🚫 Ataques a contas de CLIENTES são PROIBIDOS** — só entre as suas próprias contas.
- **Divulgação:** coordenada / restrita (sem writeups públicos). Gold Standard Safe Harbor.

## Estado da sessão (2026-09-09)

- 1 conta de teste ativa (alias wearehackerone.com, sem KYC). Tiago logado.
- 1 API key **read-only** fornecida via `code -1 coinspot` → `$SCRATCHPAD/coinspot_session.env`.
- Sem 2ª conta por ora (leads cross-account K–N ficam para depois).

## Histórico de mudanças de escopo

| Data       | Mudança |
|------------|---------|
| 2026-09-02 | escopo inicial registrado (recon ladrão de bancos) |
| 2026-09-08 | scope.md preenchido a partir do README da ficha (era template) — pendente reconferir na policy viva |
| 2026-09-09 | reconferido contra a policy viva (CSV oficial de scopes do H1). 4 assets, host www + /v2/api = critical, mobile = high. Sem mudança material. Apex sem www saiu da lista de assets (só redirect). Corrigido bug de parser (asset entre crases abaixo de "Out of scope"). |
