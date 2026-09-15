# Contratos versionados

- [`scope-manifest.schema.json`](scope-manifest.schema.json): JSON Schema autoritativo de `scope.json`.
- [`scope-manifest.md`](scope-manifest.md): normalização, matching, aprovação e exemplos de escopo.
- [`control-records.md`](control-records.md): manifesto da collection, writer, revisões, aprovações e tombstones.
- [`ingestion-report.md`](ingestion-report.md): resultados por arquivo, contadores, falhas parciais e resposta CLI/MCP da ingestão.

Alterações incompatíveis exigem nova versão de schema e migração explícita; não se altera silenciosamente o significado de dados persistidos.
