# Contrato de leitura de escopo v1

`hive scope status <program_id> --json` emite somente um objeto JSON em stdout, validado pelo [schema compartilhado v1](scope-status-v1.schema.json):

```json
{"schema_version":1,"program_id":"lab","status":"approved","scope_revision":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","rules":[{"action":"include","asset_type":"host","value":"api.example.test"}]}
```

`status` é `approved` ou `unapproved`. Sem aprovação, `scope_revision` é `unapproved` e `rules` é `[]`. Com aprovação, a revisão é SHA-256 hexadecimal minúsculo de 64 caracteres e regras são `include`/`exclude` com `asset_type` `host`, `wildcard_domain`, `ip`, `cidr` ou `url_prefix` e `value` normalizado. Nenhum outro campo, motivo livre, token, caminho local ou documento comum é emitido. O JSON precisa caber em 1 MiB neste incremento.

O servidor reconstrói o documento de escopo publicado que corresponde à aprovação ativa, verifica o hash do JSON normalizado, identidade, integridade de chunks e a revisão de aprovação novamente antes de responder. Aprovações antigas sem `scope_document_hash` exigem nova aprovação explícita para esse reader; não há atualização automática. Falha de infraestrutura, documento incompleto, manifesto inválido ou revisão alterada não produz resposta `approved`. O endpoint HTTP é `GET /api/v1/scopes/{programID}/status`, autenticado e limitado à concessão de leitura do programa no JWT versionado do Center. A [ADR 005](../../decisions/005-enforcement-de-programas-no-servidor.md) substitui a autorização administrativa inicial da ADR 004.
