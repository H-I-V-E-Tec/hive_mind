# Unidade canônica, domínio de deduplicação e adaptadores — versão 1

Estado: proposta. Define o modelo de dados, a chave de unicidade, a política de retenção e o contrato dos adaptadores que o conversor deve implementar. Os objetos canônicos deste contrato ainda não são persistidos; a persistência escolhida está na [decisão 003](../../decisions/003-canonical-persistence.md). A entrega inicial de 2026-09-17 implementa somente o [envelope de ingestão](ingestion-document.schema.json), extração de blocos e projeção no Qdrant com localizador/hash por chunk; esses campos não constituem uma `CanonicalUnit` nem garantem unicidade. Complementa [converter-pipeline.md](converter-pipeline.md), que descreve as fronteiras de responsabilidade.

## Objetos

| Objeto | Papel | Campos obrigatórios |
| --- | --- | --- |
| `SourceRevision` | Identifica de onde veio o material, quem o publicou e em qual versão. | `source_id`, `revision`, `raw_hash`, `hive_id`, `program_id`, `access_partition`, `classification`, `source_locator`, `writer_device_id`, `format`, `collected_at`, `ingested_at` |
| `ExtractedBlock` | Trecho localizável produzido por um adaptador. | `source_revision`, `ordinal`, `kind`, `text` ou `structured_value`, `locator`, `extraction_status`, `converter_fingerprint` |
| `CanonicalUnit` | Conteúdo admitido uma vez dentro do seu domínio de acesso. | `schema_version`, `unit_id`, `unit_kind`, `normalized_content`, `semantic_qualifiers`, `canonical_hash`, `normalizer_fingerprint`, `hive_id`, `program_id`, `access_partition`, `classification`, `validation_state`, `created_at` |
| `Occurrence` | Vincula uma unidade à passagem que a sustenta. | `occurrence_id`, `unit_id`, `source_revision`, `block_ordinal`, `locator`, `writer_device_id`, `observed_at` (opcional), `ingested_at`, `state` |
| `Relation` | Conexão explícita entre unidades. | `relation_id`, `from_unit`, `to_unit`, `kind`, `origin`, `evidence_ref`, `created_at`, `reviewer_ref` (opcional) |
| `AdmissionDecision` | Resultado da política de admissão para um bloco/unidade candidata. | `decision`, `reason_codes`, `policy_version`, `canonical_unit_ref` (opcional), `provenance_ref` (opcional), `reviewer_ref` (opcional), `decided_at` |

Enumerações:

- `unit_kind`: `observation`, `evidence`, `hypothesis`, `procedure`, `result`. Afirmações extraídas automaticamente entram como `observation` ou `hypothesis`; nenhuma extração automática cria `result`.
- `classification`: `internal`, `restricted`, `unknown` — os mesmos valores de `server/metadata.go`. `unknown` é uma representação definida, não ausência.
- `Relation.kind`: `supports`, `contradicts`, `supersedes`, `derived_from`, `related_to`.
- `Relation.origin`: `manual`, `rule`, `model_suggested`. Relações `model_suggested` exigem `reviewer_ref` antes de serem visíveis fora da revisão.
- `Occurrence.state`: `active`, `pending_delete`, `deleted` — os mesmos estados de `document_head` em [control-records.md](control-records.md).
- `validation_state`: `admitted`, `pending_review`, `rejected`.
- `extraction_status`: `complete`, `partial`, `failed`.
- `decision`: `new`, `duplicate`, `additional_evidence`, `update`, `review_required`, `rejected`.

## Identidades

Identificadores são UUID v5 determinísticos, calculados como em `server/revision.go` (`deterministicUUID`: partes concatenadas com `\x00` sob o namespace URL), mantendo a convenção de [control-records.md](control-records.md) e da spec 02.

| Identificador | Derivação |
| --- | --- |
| `source_id` | `UUIDv5("source", hive_id, program_id, source_locator normalizado)` — coincide com o `document_id` atual para arquivos em `programs/<id>/`. |
| `revision` | SHA-256 de (`raw_hash`, `converter_fingerprint`) — o equivalente do `document_revision` atual para o conversor. |
| `unit_id` | `UUIDv5("unit", hive_id, program_id, access_partition, classification, unit_kind, normalizer_fingerprint, canonical_hash)` — função pura da chave de unicidade. |
| `occurrence_id` | `UUIDv5("occurrence", unit_id, source_revision, block_ordinal)`. |
| `relation_id` | `UUIDv5("relation", from_unit, to_unit, kind)`. |
| Ponto no índice | `UUIDv5("unit-chunk", unit_id, projection_fingerprint, ordinal)`; janelas de recuperação são projeções, não unidades. |

Nenhum identificador de unidade deriva do path. Renomear ou copiar uma fonte muda `source_id`/`source_locator`; não muda `unit_id`.

## Chave de unicidade e domínio de deduplicação

Chave única, com todos os componentes obrigatórios e sem nulos:

```text
(hive_id, program_id, access_partition,
 classification, unit_kind, normalizer_fingerprint, canonical_hash)
```

- `access_partition` = `UUIDv5("partition", hive_id, program_id, classification)`. Na versão 1 é totalmente derivado; existe como campo próprio para permitir, no futuro, partições mais finas (por exemplo, restrição por equipe) sem alterar a chave.
- `canonical_hash` = SHA-256 de `normalized_content` concatenado com `semantic_qualifiers` serializados de forma canônica (chaves ordenadas, sem espaços). Qualificadores que alteram o significado da afirmação (ativo, ambiente, condição, janela temporal explícita) fazem parte do hash. Horário de importação, path, writer e nome do arquivo não fazem.
- `normalizer_fingerprint` = `<algoritmo>:<versão>:<parâmetros>`, no mesmo estilo de `parserFingerprint`/`chunkerFingerprint` atuais. Mudar o normalizador cria uma nova família de unidades; a migração é explícita (seção 13 do plano).
- Colisão de hash com conteúdo diferente é tratada comparando `normalized_content` byte a byte; se diferir, a inserção falha com `reason_code = hash_collision` e vai para revisão.
- Deduplicação **nunca** cruza `hive_id`, `program_id` ou `classification`. Um candidato de outra partição não é consultado, retornado nem contabilizado; a existência de conteúdo restrito não pode ser inferida por uma importação em partição `internal`.
- Ocorrências são únicas por (`unit_id`, `source_revision`, `block_ordinal`). Reimportar a mesma revisão com o mesmo `converter_fingerprint` e `normalizer_fingerprint` não cria unidades, ocorrências nem trabalhos de embedding.

## Tempo e estado

| Campo | Significado | Origem permitida |
| --- | --- | --- |
| `observed_at` | Quando o fato ocorreu ou foi observado. | Metadado explícito da fonte (`collected_at`, timestamp de log). Ausente quando não informado; nunca inferido. |
| `ingested_at` | Quando a ocorrência entrou no Hive. | Relógio do writer no momento da admissão. |
| `valid_from` / `valid_until` | Janela declarada de validade, quando existir. | Qualificador explícito; ausente por padrão. |
| `validation_state` | Estado de revisão da unidade. | Política de admissão ou revisor. |

Uma observação mais recente não invalida uma anterior automaticamente; a relação `supersedes` é criada por regra explícita ou revisor, sempre com `evidence_ref`.

## Retenção

| Dado | Regra da versão 1 |
| --- | --- |
| Original da fonte | Referenciado por `raw_hash` + `source_locator`; não copiado por padrão. Localizadores de arquivo local devem indicar `device_local: true` quando não acessíveis a outro dispositivo. |
| Cópia de original | Somente quando a política do programa exigir retenção de evidência; uma cópia por (`raw_hash`, `access_partition`) em armazenamento privado. Duplicatas nunca criam novo blob. |
| Fila de revisão | Guarda apenas os blocos candidatos, decisão proposta e motivos; acesso restrito a writers da partição; expira conforme o perfil de implantação. |
| Arquivos temporários de importação | Descartados ao final do job, com ou sem sucesso. |
| Unidade sem ocorrências ativas | Marcada `pending_delete`; remoção física exige zero ocorrências ativas, carência cumprida e `tombstone` (mesmo registro de [control-records.md](control-records.md)). |
| Vetores | Removidos junto com a unidade; recriáveis a partir de `normalized_content` enquanto a unidade existir. |
| Logs de decisão | `decision`, `reason_codes`, identificadores e `policy_version`; nunca `normalized_content` nem conteúdo sensível. |

Prazos numéricos (carência, expiração da fila, retenção de blobs) pertencem ao [perfil de implantação](../../operations/deployment-profile.md), não a este contrato; hoje estão `UNSET`.

## Contrato dos adaptadores

Um adaptador é um componente registrado que declara:

| Campo | Conteúdo |
| --- | --- |
| `formats` | Extensões e assinaturas (magic bytes) atendidas. Detecção usa extensão **e** assinatura **e** validação do parser; nome do arquivo sozinho não seleciona o adaptador. |
| `version` | Componente do `converter_fingerprint`. |
| `limits` | Bytes máximos, profundidade/elementos (para formatos aninhados), tempo máximo e memória; abaixo dos limites globais quando o formato exigir. |
| `fidelity` | `full`, `partial` ou `lossy` — o que o adaptador garante preservar. |
| `requires` | Recursos externos (binário de OCR, rede, credencial). Ausência do recurso torna o adaptador indisponível, com diagnóstico, não uma extração silenciosa parcial. |

Entrada: `io.Reader` limitado, `SourceRevision` já validado e contexto de importação confiável (Hive, programa, classificação, writer). Saída: sequência ordenada de `ExtractedBlock` com `locator` obrigatório e `extraction_status` por bloco; erro tipado quando o conteúdo é inválido para o formato.

Obrigações:

- não executar scripts, macros, includes ou referências externas do conteúdo;
- não ler outros arquivos além do stream recebido;
- não alterar programa, classificação ou escopo com base no conteúdo;
- suportar cancelamento por contexto e emitir blocos em streaming quando o formato permitir;
- ser determinístico: mesma entrada, mesma versão e mesmos parâmetros produzem os mesmos blocos.

Adaptadores da onda inicial (MD, TXT, JSON, JSONL, CSV, TSV, stdin) devem manter os limites atuais (`HIVE_MAX_FILE_BYTES`, `HIVE_JSON_MAX_DEPTH`, `HIVE_JSON_MAX_ELEMENTS`) como teto.

## Política de admissão: decisões e motivos

| Decisão | Efeito |
| --- | --- |
| `new` | Cria unidade e ocorrência. |
| `duplicate` | Reutiliza unidade; cria ocorrência apenas se (`source_revision`, `block_ordinal`) for nova. |
| `additional_evidence` | Reutiliza unidade e cria ocorrência de fonte independente; não altera conteúdo. |
| `update` | Cria nova unidade e relação `supersedes` com a anterior; ambas permanecem. |
| `review_required` | Nada é publicado; candidato vai à fila de revisão. |
| `rejected` | Nada é publicado; motivo registrado. |

`reason_codes` iniciais (múltiplos por decisão):

| Código | Situação |
| --- | --- |
| `empty_content` | Bloco sem texto recuperável. |
| `missing_locator` | Evidência sem localizador verificável. |
| `extraction_partial` | Adaptador reportou `partial`. |
| `secret_detected` | Conteúdo sensível encontrado; representação sanitizada obrigatória. |
| `sanitization_destroyed_meaning` | Sanitização removeu o conteúdo útil. |
| `instruction_content` | Conteúdo tenta conceder autoridade, escopo ou instruções ao sistema. |
| `hash_collision` | Mesmo `canonical_hash`, conteúdo diferente. |
| `semantic_candidate` | Similaridade acima do limiar com unidade existente; requer revisão. |
| `conflicting_claim` | Candidato contradiz unidade existente. |
| `temporal_change` | Mesma afirmação com tempo/estado diferente. |
| `cross_partition_blocked` | Tentativa de vínculo fora da partição (sempre rejeitada). |
| `policy_version_changed` | Fonte já processada com política anterior; reavaliação. |

Um score de similaridade nunca é motivo suficiente para `duplicate`; só `canonical_hash` igual com conteúdo confirmado produz `duplicate` automaticamente. Reversão de decisões (desfazer fusão) recria as ocorrências na unidade correta e registra a decisão anterior; não apaga histórico.

## Aceite

- Reimportar o mesmo lote com as mesmas versões: zero novas unidades, ocorrências ou embeddings.
- Duas cópias em paths diferentes: uma unidade, duas ocorrências.
- Mesmo conteúdo em programas ou classificações diferentes: duas unidades, sem vínculo.
- Pares rotulados em `server/testdata/inventory/pairs.json`: `exact_duplicate` → `duplicate`; `complement`, `contradiction`, `temporal_change` → nunca `duplicate` automático.
- Inserção concorrente da mesma chave por dois writers: uma unidade, duas ocorrências, sem perda de proveniência — exige a garantia de banco da decisão 003.
