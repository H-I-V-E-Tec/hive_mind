---
program_id: etoro
document_type: note
claimed_scope_status: unknown
classification: internal
source: bugcrowd policy (etoro) — 2026-09-09
collected_at: 2026-09-12T00:00:00Z
tags: [etoro, recon]
asset_refs: [etoro.com]
---
# eToro — registro de engagement (Bugcrowd)

O programa exige `X-Bug-Bounty:<username>` em todo tráfego + registro de IP/UA/username (podem pedir).

- **Username Bugcrowd:** `moldret`
- **Header enviado em todo tráfego:** `X-Bug-Bounty: moldret`
- **IP de saída:** `177.40.95.165`
- **User-Agent (probe):** `Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0 Safari/537.36`
- **Início da fase ativa:** 2026-09-05 00:42 -03
- **Ferramentas:** httpx (probe de hosts vivos, rate baixo), katana (crawl leve) — **sem tooling de volume/stress/DoS**.
- **Escopo tocado:** `*.etoro.com`, `etorox.com`, `etoropartners.com`, `delta.app` (in-scope).

## Conta de teste
- **E-mail:** `moldretvalor726333@bugcrowdninja.com`
- **Username:** `moldretvalor7284` · **user_id:** `49462268` · **cid:** `50640014`
- **Estado:** conta **REAL** (não demo), **onboarding travado em verificação de telefone** (`/api/onboarding/v3/changePhone/.../initiate-phone-verification` → 429 CF-1015). Sem KYC/telefone → sem API Key, app gated.
- **Sessão web:** bearer JWE (`AccountType: Real`) capturado do DevTools (guardado só no scratchpad, efêmero). Protegido por DataDome + Cloudflare `cf_clearance`.

## Observação de segurança (2026-09-05)
- **Cross-surface token:** o bearer da sessão **web** é **aceito** pelo `public-api.etoro.com` (developer API) — retorna `403 InsufficientPermissions`, não `401`. Ou seja, o token vale nos dois mundos, mas **sem scopes** no public-api. Enforcement de scope **consistente** (5/5 endpoints = 403). Não é bug isolado, mas confirma audiência de token compartilhada — anotar p/ chain.

## Log de sessões ativas
| Data | Ação | Hosts | Rate | Obs |
|------|------|-------|------|-----|
| 2026-09-05 | httpx probe dos 574 subdomínios | 574 | ~10 req/s | fase 1 (descoberta de vivos) |
| 2026-09-05 | 6 GETs public-api com bearer de sessão | public-api | serial+sleep | mapa de scope: 5/5 = 403 InsufficientPermissions (token web sem scope no public-api) |
| 2026-09-05 | `get-my-profile-and-scopes` via MCP com o mesmo bearer (Tiago rodou, regra de delegação) | mcp.public-api | 1 request | **6/6 = 403 InsufficientPermissions.** MCP é proxy puro (`authChannel:"bearer"`, relay honesto pro mesmo backend). **Fio fechado:** web bearer não tem scope pra Public API/MCP em nenhum caminho testado. Precisa de API key formal com scope → bloqueado até onboarding (telefone) destravar. |
