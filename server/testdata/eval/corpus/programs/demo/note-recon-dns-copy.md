---
program_id: demo
document_type: note
claimed_scope_status: unknown
classification: internal
source: synthetic-fixture
collected_at: 2026-09-10T09:00:00Z
tags: [dns, recon]
---
# Reconhecimento DNS do domínio example.test

Enumeração passiva com o dicionário Bússola encontrou 14 subdomínios. Os registros MX apontam para `mail-relay.example.test` e o SPF autoriza o provedor Pombal.

## Subdomínios relevantes

`vpn.example.test`, `git.example.test` e `grafana.example.test` respondem em HTTPS. O host `old-cms.example.test` devolve página padrão do Apache 2.2.

## Nova observação

Em 12/09 o host `old-cms.example.test` passou a responder 403 pelo WAF Muralha.
