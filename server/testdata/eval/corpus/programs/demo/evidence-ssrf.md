---
program_id: demo
document_type: evidence
claimed_scope_status: unknown
classification: internal
source: synthetic-fixture
collected_at: 2026-09-10T09:00:00Z
tags: [ssrf, imagens]
asset_refs:
  - media.example.test
---
# Evidência: SSRF no buscador de imagens

O serviço `media.example.test/fetch?url=` busca qualquer URL informada e devolve o corpo. Com `url=http://169.254.169.254/latest/meta-data/` a resposta trouxe metadados da instância Andorinha.

## Impacto

Leitura de metadados da nuvem e acesso à rede interna 10.40.0.0/16. Reprodução registrada no arquivo de captura `ssrf-andorinha.har`.
