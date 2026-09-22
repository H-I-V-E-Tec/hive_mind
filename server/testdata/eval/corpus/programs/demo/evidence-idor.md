---
program_id: demo
document_type: evidence
claimed_scope_status: unknown
classification: internal
source: synthetic-fixture
collected_at: 2026-09-10T09:00:00Z
tags: [idor, faturas]
asset_refs:
  - billing.example.test
---
# Evidência: IDOR em faturas

`GET /v1/invoices/{id}` em `billing.example.test` devolve faturas de outros clientes ao incrementar o identificador numérico. Testado com a conta Garça acessando as faturas 1042 a 1050.

## Impacto

Exposição de dados de cobrança de terceiros. Sem limite de taxa no endpoint.
