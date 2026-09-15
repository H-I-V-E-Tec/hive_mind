# Contratos versionados

- [`scope-manifest.schema.json`](scope-manifest.schema.json): JSON Schema autoritativo de `scope.json`.
- [`scope-manifest.md`](scope-manifest.md): normalização, matching, aprovação e exemplos de escopo.
- [`control-records.md`](control-records.md): manifesto da collection, writer, revisões, aprovações e tombstones.
- [`ingestion-report.md`](ingestion-report.md): resultados por arquivo, contadores, falhas parciais e resposta CLI/MCP da ingestão.
- [`inventory-report.md`](inventory-report.md): diagnóstico local de formatos, cobertura e candidatos a cópia integral.
- [`converter-pipeline.md`](converter-pipeline.md): proposta dos contratos entre adaptador, normalização, admissão e persistência; implementação pendente.

Alterações incompatíveis exigem nova versão de schema e migração explícita; não se altera silenciosamente o significado de dados persistidos.
