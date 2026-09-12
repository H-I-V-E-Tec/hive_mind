---
program_id: yahoo
document_type: note
claimed_scope_status: unknown
classification: internal
source: intigriti policy (yahoo) — 2026-09-08
collected_at: 2026-09-12T00:00:00Z
tags: [overview, yahoo]
asset_refs: [apis.mail.yahoo.com]
---
# Visão geral — yahoo

Metadados do alvo (de `target.yml`):

```yaml
# Metadados do alvo — fonte do RANKING.md
programa: "Yahoo (Paranoids)"
plataforma: intigriti
url: "https://app.intigriti.com/programs/yahoo"
slug: yahoo
tipo: "bounty"
status: "recon"
data_inicio: 2026-09-07
ultima_atividade: 2026-09-08
escopo_wildcard: true
faixa_bounty: { low: 3000, high: 15000 }
tags: [web, api, wildcard, payments, oauth]
score:
  bounty: 5          # Except $12.5-15k · Crit $10-12.5k
  escopo: 5          # 2 wildcards + ~30 produtos (Mail/Finance/Sports Fantasy/etc.)
  superficie: 4      # payment/oauth/admin ecosystem + SSRF testbed "bananastand"
  atividade: 3       # mega-programa legado
  familiaridade: 3   # BAC/IDOR/authz + money/lógica (Fantasy Wallet)
  oportunidade: 3    # saturação de década → mirar wildcards frescos + Fantasy Wallet + SSRF
notas: "Recon padronizado (subfinder+keys+DoH) destrava payment/oauth. Benchmark externo em recon-benchmarks."

```
