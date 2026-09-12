---
program_id: coinspot
document_type: note
claimed_scope_status: unknown
classification: internal
source: hackerone policy (coinspot) — CSV export 2026-09-09
collected_at: 2026-09-12T00:00:00Z
tags: [coinspot, overview]
asset_refs: [www.coinspot.com.au]
---
# Visão geral — coinspot

Metadados do alvo (de `target.yml`):

```yaml
# Metadados do alvo — fonte do RANKING.md
programa: "CoinSpot"
plataforma: hackerone
url: "https://hackerone.com/coinspot"
slug: coinspot
tipo: "bounty"
status: "prospecto"
data_inicio: 2026-09-02
ultima_atividade: 2026-09-02
escopo_wildcard: false          # a confirmar com o escopo do Mac
faixa_bounty: { low: 0, high: 0 }   # a confirmar
tags: [web, api, crypto]

# Score: cada fator de 1 a 5 (ver fórmula no README.md da raiz)
# Seed pela prioridade da curadoria Sword + ajuste pela nota — revisar com recon.
score:
  bounty: 4
  escopo: 4
  superficie: 4
  atividade: 4
  familiaridade: 4
  oportunidade: 5

notas: "Seed a partir da curadoria Sword (notas/sword-curadoria.md); score provisório — revisar com o recon do Mac (~/Documents/Sword/targets/coinspot/)."

```
