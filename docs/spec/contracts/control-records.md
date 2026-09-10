# Contrato da collection de controle v1

`${HIVE_COLLECTION}__control` não contém vetores pesquisáveis nem conteúdo de documentos. Os IDs são UUID v5 determinísticos por `hive_id`, `record_type` e chave lógica. Toda escrita usa confirmação `wait=true` e controle otimista pela versão anterior; conflito falha sem sobrescrever silenciosamente.

## `collection_manifest`

Existe exatamente um por Hive:

| Campo | Regra |
| --- | --- |
| `schema_version` | `1`. |
| `hive_id`, `data_collection`, `control_collection` | IDs validados e imutáveis. |
| `embedding_provider` | `ollama` na v0.1. |
| `embedding_model`, `embedding_model_digest` | Nome e digest exatos do modelo local. |
| `vector_dimension`, `distance`, `normalization` | Configuração vetorial efetiva. |
| `parser_version`, `chunker_version`, `payload_schema_version` | Versões que participam da compatibilidade. |
| `created_at` | RFC 3339 UTC. |

`validate` compara todos os campos. Mudança exige migração explícita para outra collection ou rebuild controlado; nunca altera o manifesto existente por conveniência.

## `writer_registration`

Contém `hive_id`, único `writer_device_id`, data da atribuição e identificador sanitizado da aprovação operacional. Um processo writer com outro device ID falha antes de ingerir. Promoção é uma operação administrativa explícita, executada depois que a credencial e identidade de rede do writer anterior forem revogadas.

## `document_head`

Um registro por `document_id`, contendo `program_id`, path relativo, `active_document_revision`, `active_scope_revision`, quantidade de chunks, estado (`active`, `pending_delete` ou `deleted`) e timestamps UTC.

O commit altera esse registro somente depois que todos os chunks da combinação de revisões estiverem confirmados. Consultas descartam candidatos que não coincidam com o head. Reconciliadores podem apagar combinações não referenciadas, mas nunca a combinação ativa.

## `scope_approval`

Um registro por programa, contendo `program_id`, `status` (`approved` ou `unapproved`), `scope_revision`, schema version, `approved_by_device_id` e timestamps UTC. `scope_revision` é o SHA-256 dos bytes exatos de `scope.json` aprovado.

Ao detectar que o arquivo atual não coincide com o hash aprovado, o writer grava `unapproved` antes de qualquer nova ingestão do programa. Uma nova aprovação só vira ativa depois que todos os chunks do programa forem materializados e confirmados sob o novo hash.

## `tombstone`

Contém `document_id`, última revisão, motivo enumerado, `deleted_by_device_id` e timestamp UTC. O tombstone é confirmado antes da remoção dos chunks. Reingestão do mesmo path exige operação explícita que revogue o tombstone e gere evento de auditoria.

## Aceite

- Falha em cada fronteira de commit conserva um head completo anteriormente ativo ou publica um novo conjunto completo.
- Manifesto incompatível, segundo writer, aprovação alterada e tombstone falham fechados.
- Registros de controle não contêm texto, embedding, segredo, path absoluto ou consulta do usuário.
