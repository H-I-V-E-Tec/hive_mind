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

Um registro por writer, com chave lógica igual a `writer_device_id`. Contém `hive_id`, `writer_device_id`, `operational_approval_id` (identificador sanitizado da aprovação operacional) e `assigned_at`. O registro é criado pelo próprio writer na primeira operação de escrita e é imutável: mudar o `HIVE_WRITER_APPROVAL_ID` de um dispositivo já registrado falha em `validate`. Reader exige ao menos um registro. Revogar um writer é operação administrativa: revogar credencial e identidade de rede e registrar `audit record credential_revocation`; o registro permanece como histórico e os documentos do writer revogado continuam ativos até serem removidos por tombstone.

Registros legados com chave lógica igual a `hive_id` (v1.0) continuam válidos como registro do dispositivo que os criou.

## `document_head`

Um registro por `document_id`, contendo `program_id`, path relativo, `active_document_revision`, `active_scope_revision`, quantidade de chunks, estado (`active`, `pending_delete` ou `deleted`), timestamps UTC, `writer_device_id` (dono) e `document_bytes` (tamanho do arquivo original, somente para métricas da spec 09).

`writer_device_id` é definido na primeira publicação e preservado por toda reescrita do head, inclusive rematerialização de escopo. Somente o dono altera o head; head sem dono (v1.0) é adotado pelo primeiro writer que o reescrever.

O commit altera esse registro somente depois que todos os chunks da combinação de revisões estiverem confirmados. Consultas descartam candidatos que não coincidam com o head. Reconciliadores podem apagar combinações não referenciadas, mas nunca a combinação ativa.

## `scope_approval`

Um registro por programa, contendo `program_id`, `status` (`approved` ou `unapproved`), `scope_revision`, schema version, `approved_by_device_id` e timestamps UTC. `scope_revision` é o SHA-256 dos bytes exatos de `scope.json` aprovado.

`approved_by_device_id` é o dono da aprovação. Somente ele compara o arquivo local com o hash aprovado: ao detectar divergência, grava `unapproved` antes de qualquer nova ingestão do programa. Writers não donos usam a revisão registrada e reconstroem o manifesto a partir do documento de escopo publicado; nunca invalidam. Uma nova aprovação só vira ativa depois que todos os chunks do programa forem materializados e confirmados sob o novo hash.

## `tombstone`

Contém `document_id`, última revisão, motivo enumerado, `deleted_by_device_id` e timestamp UTC. O tombstone é confirmado antes da remoção dos chunks. Reingestão do mesmo path exige operação explícita que revogue o tombstone e gere evento de auditoria.

## Aceite

- Falha em cada fronteira de commit conserva um head completo anteriormente ativo ou publica um novo conjunto completo.
- Manifesto incompatível, writer não registrado, escrita em documento de outro writer, aprovação alterada pelo dono e tombstone falham fechados.
- Registros de controle não contêm texto, embedding, segredo, path absoluto ou consulta do usuário.
