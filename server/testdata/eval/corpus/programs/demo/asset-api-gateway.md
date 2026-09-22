---
program_id: demo
document_type: asset
claimed_scope_status: unknown
classification: internal
source: synthetic-fixture
collected_at: 2026-09-10T09:00:00Z
tags: [gateway, tls]
asset_refs:
  - gateway.example.test
---
# Gateway de API gateway.example.test

O gateway público responde na porta 8443 com certificado wildcard emitido pela CA interna Ferrugem. O balanceador Kestrel encaminha para os pods do cluster `orion` e injeta o cabeçalho `X-Orion-Trace`.

## Versões observadas

Banner HTTP identifica Kestrel 4.2.1. O endpoint `/healthz` expõe o build `orion-2026.08.3` sem autenticação.
