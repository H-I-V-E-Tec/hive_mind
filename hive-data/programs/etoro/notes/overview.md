---
program_id: etoro
document_type: note
claimed_scope_status: unknown
classification: internal
source: bugcrowd policy (etoro) — 2026-09-09
collected_at: 2026-09-12T00:00:00Z
tags: [etoro, overview]
asset_refs: [etoro.com]
---
# Visão geral — etoro

Metadados do alvo (de `target.yml`):

```yaml
# Metadados do alvo — fonte do RANKING.md
programa: "eToro"
plataforma: bugcrowd
url: "https://bugcrowd.com/etoro"
slug: etoro
tipo: "bounty"
status: "recon"
data_inicio: 2026-09-02
ultima_atividade: 2026-09-05
escopo_wildcard: true
faixa_bounty: { low: 1500, high: 15000 }
tags: [web, api, fintech, wildcard]
score:
  bounty: 4          # P1 $6k-15k · P2 $1.5k-6k
  escopo: 5          # wildcard *.etoro.com fintech
  superficie: 4      # copy-trading, carteira, afiliados (por.etoro.com), backend affapi
  atividade: 4       # triagem 2 dias
  familiaridade: 3   # BAC/IDOR + lógica
  oportunidade: 3    # main já garimpado; ramos frescos (afiliados/B2C)
notas: "Header obrigatório X-Bug-Bounty. Ramo por.etoro.com mapeado. Wildcard = recon nível 3."

```
