# 02 — Ingestão de documentos de recon

## Resultado

O Hive Mind indexa somente `.md`, `.txt` e `.json` da pasta Hive, produzindo chunks com contexto e proveniência estável.

## Comportamento

Markdown é dividido por seções, mantendo título e bloco de metadados no chunk correspondente. Texto usa janelas com sobreposição configurável. JSON válido é normalizado para uma representação legível, determinística e pesquisável. Binários, arquivos ignorados e itens acima do limite são recusados com diagnóstico seguro.

Cada ponto contém `hive_id`, `path` relativo, `content_hash` e `indexed_at`. IDs são determinísticos por arquivo/chunk. Arquivos inalterados não geram upsert; uma atualização remove os pontos antigos antes de escrever os novos.

## Segurança

O caminho relativo não pode escapar de `HIVE_DATA_DIR`; logs registram identificação e motivo de erro sem conteúdo do documento.

## Aceite e testes

- Fixtures Markdown preservam cabeçalho e seção; JSON permite consulta por chave e valor.
- Reingestão é idempotente e atualização não deixa chunks obsoletos.
- Testes cobrem path traversal, binário, tamanho máximo e `.gitignore`.
