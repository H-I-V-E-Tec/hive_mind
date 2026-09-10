# 02 — Ingestão de documentos de recon

## Resultado

O Hive Mind indexa somente `.md`, `.txt` e `.json` da pasta Hive, produzindo chunks com contexto e proveniência estável.

## Comportamento

Markdown é dividido por seções, mantendo título e bloco de metadados no chunk correspondente. Texto usa janelas com sobreposição configurável. JSON válido é normalizado com chaves ordenadas, números preservados e limites de profundidade e quantidade de elementos, produzindo representação legível, determinística e pesquisável. Binários, arquivos ignorados e itens acima dos limites são recusados com diagnóstico seguro.

Os padrões seguros são: `HIVE_MAX_FILE_BYTES=5242880`, `HIVE_MAX_CHUNKS_PER_FILE=1000`, `HIVE_CHUNK_MAX_CHARS=2000`, `HIVE_CHUNK_OVERLAP_CHARS=200`, `HIVE_JSON_MAX_DEPTH=64`, `HIVE_JSON_MAX_ELEMENTS=100000`, `HIVE_MAX_EMBEDDING_WORKERS=2` e `HIVE_DELETE_GRACE_HOURS=24`. Configuração pode reduzir esses valores. Aumentá-los, até os tetos absolutos documentados na ajuda (50 MiB, 5.000 chunks, 8.000 caracteres e 16 workers), exige valor explícito e aparece em `validate`; sobreposição nunca excede metade do chunk. Profundidade JSON, quantidade de elementos e carência de exclusão não possuem override acima desses padrões na v0.1.

O hash de conteúdo é SHA-256 dos bytes originais. `document_id` é um UUID v5 derivado de `hive_id`, `program_id` e path relativo normalizado; `document_revision` deriva do hash de conteúdo, versão do parser e versão do chunker. `scope_revision` identifica o hash do manifesto aprovado usado para materializar o escopo, ou `unapproved`. O ID de ponto é um UUID v5 derivado de `document_id`, `document_revision`, `scope_revision` e ordinal do chunk. Cada ponto contém esses campos, `hive_id`, `path`, `content_hash`, `indexed_at` UTC e os metadados da spec 03.

## Publicação de uma revisão

1. O writer lê e valida o arquivo por um descritor seguro, calcula todos os chunks e conclui todos os embeddings sem alterar a revisão ativa.
2. Grava os pontos da nova revisão com confirmação `wait=true` e verifica a quantidade persistida.
3. Atualiza, também com confirmação, o registro de `document_id` na collection de controle para apontar para a nova combinação de revisão de documento e escopo.
4. Somente após o commit remove os pontos da revisão anterior. Falha na limpeza deixa pontos inativos recuperáveis e gera pendência de reconciliação; não invalida a revisão ativa.

Arquivos inalterados não geram upsert. Falha antes do commit mantém a revisão anterior ativa. Falha depois do commit nunca reativa silenciosamente a revisão antiga.

Ausência observada pelo watcher apenas marca o documento como `pending_delete`. Exclusão efetiva exige `hive-mind remove <path>` ou `ingest --prune` depois do período de carência configurado, grava primeiro um tombstone na collection de controle e só então remove os chunks. Isso evita interpretar atraso de sincronização como exclusão intencional.

## Segurança

Somente o writer ingere. O caminho real não pode escapar de `HIVE_DATA_DIR`; symlinks externos, dispositivos, sockets e troca do alvo entre validação e leitura devem falhar fechados. Logs registram identificação e motivo de erro sem conteúdo do documento.

## Aceite e testes

- Fixtures Markdown preservam cabeçalho e seção; JSON permite consulta por chave e valor.
- Reingestão é idempotente; atualização bem-sucedida só publica a revisão completa e reconcilia chunks inativos.
- Falhas antes, durante e depois de cada etapa de publicação preservam uma revisão consultável e correta.
- Testes cobrem path traversal, troca de symlink, arquivo especial, binário, JSON profundo, tamanho/quantidade máximos, `.gitignore`, tombstone e atraso de sincronização.
