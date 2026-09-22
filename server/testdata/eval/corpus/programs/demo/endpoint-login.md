---
program_id: demo
document_type: endpoint
claimed_scope_status: unknown
classification: internal
source: synthetic-fixture
collected_at: 2026-09-10T09:00:00Z
tags: [auth, jwt]
asset_refs:
  - gateway.example.test
---
# Endpoint de login POST /v2/session

O endpoint `/v2/session` aceita JSON com `username` e `password` e devolve um JWT assinado com HS256. O limite de tentativas é 20 por minuto por IP, aplicado pelo módulo Cadeado.

## Observações

Após cinco falhas o campo `lockout_seconds` aparece na resposta com valor 300. Tokens expiram em 15 minutos; o refresh usa o cookie `orion_refresh`.
