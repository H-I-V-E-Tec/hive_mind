# 02 — Ingestão de documentos de recon

## Resultado

O Hive Mind indexa `.md`, `.txt`, `.json`, `.jsonl`, `.ndjson`, `.csv` e `.tsv` da pasta Hive, produzindo chunks com contexto e proveniência estável. O comando offline `convert` transforma arquivos ou stdin no [envelope `hive-document/v1`](contracts/ingestion-document.schema.json), antes da ingestão.

## Comportamento

Markdown é dividido por seções, mantendo título e bloco de metadados no chunk correspondente. Texto usa janelas com sobreposição configurável. JSON válido é normalizado com chaves ordenadas, números preservados e limites de profundidade e quantidade de elementos, produzindo representação legível, determinística e pesquisável. Binários, arquivos ignorados e itens acima dos limites são recusados com diagnóstico seguro.

O comportamento acima permanece para MD/TXT/JSON comuns. Envelopes e novos formatos usam o conversor versionado: blocos localizáveis, números JSON exatos, tabelas com cabeçalho/células preservados e limites agregados. O envelope contém os metadados definidos pelo operador; o payload vetorial adiciona `source_format`, `source_raw_hash`, `converter_fingerprint`, `source_locator`, `block_ordinal`, `canonical_hash` e `writer_device_id`. Só o texto do chunk é embedado. O hash canônico aqui é do chunk; não implementa a unicidade global do contrato de unidades. `classification: unknown` produz aviso por arquivo e continua fora da busca.

Os padrões seguros são: `HIVE_MAX_FILE_BYTES=5242880`, `HIVE_MAX_CHUNKS_PER_FILE=1000`, `HIVE_CHUNK_MAX_CHARS=2000`, `HIVE_CHUNK_OVERLAP_CHARS=200`, `HIVE_JSON_MAX_DEPTH=64`, `HIVE_JSON_MAX_ELEMENTS=100000`, `HIVE_MAX_EMBEDDING_WORKERS=2` e `HIVE_DELETE_GRACE_HOURS=24`. Configuração pode reduzir esses valores. Aumentá-los, até os tetos absolutos documentados na ajuda (50 MiB, 5.000 chunks, 8.000 caracteres e 16 workers), exige valor explícito e aparece em `validate`; sobreposição nunca excede metade do chunk. Profundidade JSON, quantidade de elementos e carência de exclusão não possuem override acima desses padrões na v0.1.

O hash de conteúdo é SHA-256 dos bytes originais. `document_id` é um UUID v5 derivado de `hive_id`, `program_id` e path relativo normalizado; `document_revision` deriva do hash de conteúdo, versão do parser e versão do chunker. `scope_revision` identifica o hash do manifesto aprovado usado para materializar o escopo, ou `unapproved`. O ID de ponto é um UUID v5 derivado de `document_id`, `document_revision`, `scope_revision` e ordinal do chunk. Cada ponto contém esses campos, `hive_id`, `path`, `content_hash`, `indexed_at` UTC e os metadados da spec 03.

Para envelopes/novos formatos, a revisão inclui também o fingerprint do conversor e `hive-document-projection-v1`; o manifesto e as revisões legadas não são alterados implicitamente. No envelope, `content_hash` identifica o arquivo JSON armazenado e `source_raw_hash` identifica os bytes anteriores à conversão. Reprocessar a mesma fonte no mesmo path com as mesmas versões não gera embeddings adicionais.

## Publicação de uma revisão

1. O writer lê e valida o arquivo por um descritor seguro, confirma que o `document_id` não pertence a outro writer, calcula todos os chunks e conclui todos os embeddings sem alterar a revisão ativa. Documento de outro writer é ignorado com evento de auditoria `document_skipped`/`foreign_writer`; `ingest` informa quantos foram ignorados e não os trata como erro.
2. Grava os pontos da nova revisão com confirmação `wait=true` e verifica a quantidade persistida.
3. Atualiza, também com confirmação, o registro de `document_id` na collection de controle para apontar para a nova combinação de revisão de documento e escopo, gravando `writer_device_id` do dono e `document_bytes` (tamanho do arquivo original, usado somente para métricas de uso).
4. Somente após o commit remove os pontos da revisão anterior. Falha na limpeza deixa pontos inativos recuperáveis e gera pendência de reconciliação; não invalida a revisão ativa.

Arquivos inalterados não geram upsert. Falha antes do commit mantém a revisão anterior ativa. Falha depois do commit nunca reativa silenciosamente a revisão antiga.

Ausência observada pelo watcher apenas marca o documento como `pending_delete`, e somente quando o writer observador é o dono. Exclusão efetiva exige `hive-mind remove <path>` ou `ingest --prune` depois do período de carência configurado, grava primeiro um tombstone na collection de controle e só então remove os chunks; ambos agem apenas sobre documentos do próprio writer. Isso evita interpretar atraso de sincronização, ou a ausência do arquivo na pasta de outro writer, como exclusão intencional.

## Segurança

Somente writers registrados ingerem, cada um restrito aos documentos que possui. O caminho real não pode escapar de `HIVE_DATA_DIR`; symlinks externos, dispositivos, sockets e troca do alvo entre validação e leitura devem falhar fechados. Logs registram identificação e motivo de erro sem conteúdo do documento.

## Aceite e testes

- Fixtures Markdown preservam cabeçalho e seção; JSON permite consulta por chave e valor.
- Reingestão é idempotente; atualização bem-sucedida só publica a revisão completa e reconcilia chunks inativos.
- Falhas antes, durante e depois de cada etapa de publicação preservam uma revisão consultável e correta.
- Testes cobrem path traversal, troca de symlink, arquivo especial, binário, JSON profundo, tamanho/quantidade máximos, `.gitignore`, tombstone e atraso de sincronização.
- Testes cobrem dois writers: ingestão do mesmo path é ignorada pelo não dono sem alterar chunks, `ingest` conta o documento como ignorado, e `remove`/`pending_delete`/prune de documento alheio são negados.
- Testes cobrem conversão offline/stdio, CSV multilinha, números grandes, limites agregados, metadados explícitos, envelopes inválidos, proveniência e reingestão sem novos embeddings.
