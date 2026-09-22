# 003 — Banco canônico compartilhado para unidades de conhecimento

- Estado: Aceita
- Data: 2026-09-16

## Contexto

Hoje o Qdrant guarda os documentos, seus chunks e os registros de controle (`document_head`, `scope_approval`, `tombstone`). A identidade de um documento deriva do path, e uma cópia em outro path gera documento e embeddings adicionais. O plano de melhoria propõe unidades canônicas únicas por conteúdo normalizado e domínio de acesso, com múltiplos writers em máquinas diferentes.

Uma consulta ao Qdrant seguida de inserção não garante unicidade entre writers concorrentes; um mutex local ou um SQLite por dispositivo tampouco resolve a corrida entre máquinas. A [visão estratégica](../vision/hive-mind-strategic-vision.md) já prevê que o Qdrant seja índice de recuperação, não a fonte canônica.

## Decisão

1. A fonte canônica de `SourceRevision`, `CanonicalUnit`, `Occurrence`, `Relation` e `AdmissionDecision` (contrato [canonical-unit.md](../spec/contracts/canonical-unit.md)) será um banco relacional compartilhado, PostgreSQL, acessado apenas pela camada autenticada de ingestão.
2. A unicidade da chave `(hive_id, program_id, access_partition, classification, unit_kind, normalizer_fingerprint, canonical_hash)` é uma restrição única do banco; a inserção usa `INSERT ... ON CONFLICT` e a comparação de conteúdo em transação. Nenhum caminho de ingestão (CLI, watcher, MCP) escreve unidades fora dessa camada.
3. A fila de indexação vetorial vive no mesmo banco e na mesma transação da admissão. Um worker idempotente consome a fila, gera embeddings faltantes e grava pontos com IDs determinísticos no Qdrant; a revisão só é ativada após verificação dos pontos.
4. O Qdrant permanece um índice derivado e reconstruível a partir do banco canônico. Registros de controle atuais continuam válidos até a migração descrita na seção 13 do plano.
5. SQLite pode ser usado somente para cache local, `--dry-run` ou diagnóstico offline; nunca como garantia de unicidade.
6. Enquanto o banco compartilhado não estiver implantado, nenhuma funcionalidade pode alegar unicidade global de conteúdo. O inventário e os relatórios de ingestão continuam descrevendo apenas cópias integrais por hash e resultados por arquivo.

## Consequências

- Operação passa a incluir um serviço adicional com autenticação, backup, restauração e retenção próprias; o [perfil de implantação](../operations/deployment-profile.md) precisa ser estendido antes do MVP do conversor, e as specs 06–08 permanecem pendentes até evidência em infraestrutura real.
- Banco e Qdrant não formam uma transação única; a fila transacional e a reconciliação tornam essa lacuna explícita e testável (falha antes/depois da ativação, job antigo após tombstone).
- Testes de concorrência entre writers exigem integração real com o banco; mocks em memória validam apenas o contrato da camada.
- Migração do acervo atual segue o plano: backup verificável, conversão em modo de análise, collection de destino versionada e janela de rollback com reaplicação de escritas.
- Esta decisão substitui a redação anterior de [converter-pipeline.md](../spec/contracts/converter-pipeline.md), que deixava a escolha de persistência em aberto.
