# Contratos versionados

- [`scope-manifest.schema.json`](scope-manifest.schema.json): JSON Schema autoritativo de `scope.json`.
- [`scope-manifest.md`](scope-manifest.md): normalização, matching, aprovação e exemplos de escopo.
- [`control-records.md`](control-records.md): manifesto da collection, writer, revisões, aprovações e tombstones.
- [`ingestion-report.md`](ingestion-report.md): resultados por arquivo, contadores, falhas parciais e resposta CLI/MCP da ingestão.
- [`inventory-report.md`](inventory-report.md): diagnóstico local de formatos, cobertura e candidatos a cópia integral.
- [`converter-pipeline.md`](converter-pipeline.md): fronteiras de responsabilidade entre adaptador, normalização, admissão e persistência; implementação pendente.
- [`eval-report.md`](eval-report.md): consultas rotuladas e relatório de Recall@k, MRR, repetição no top-k e custo de embeddings; harness offline.
- [`canonical-unit.md`](canonical-unit.md): unidade canônica, chave de unicidade, domínio de deduplicação, retenção, contrato dos adaptadores e motivos de admissão; proposta.

Alterações incompatíveis exigem nova versão de schema e migração explícita; não se altera silenciosamente o significado de dados persistidos.
