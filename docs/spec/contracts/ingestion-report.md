# Relatório de ingestão — versão 1

Contrato de `IngestWorkspaceReport`, usado pela CLI `ingest` e pelo MCP `ingest_workspace`. Não altera o schema dos documentos ou das collections.

## Envelope

- `schema_version`: `1`.
- `ok`: execução concluída sem erro. Documentos alheios ignorados não são erros.
- `exit_code`: `0` em sucesso, `15` para falha individual/cancelamento de lote, ou código operacional de falha anterior ao lote/poda.
- `summary`: seleção, contadores e resultados por arquivo.
- `prune_run`: indica se a poda foi chamada; pode ser verdadeiro mesmo quando ela falha.
- `pruned`: remoções retornadas como concluídas pela poda. Uma remoção parcial/tombstone requer reconciliação e não é contada como remoção completa.
- `error`: diagnóstico agregado sanitizado, presente quando `ok` é falso.

CLI: JSON em stdout, seguido de saída com `exit_code`. Falhas de configuração ou de construção/validação inicial do cliente mantêm o tratamento operacional anterior à geração deste relatório.

MCP: JSON serializado em `result.content[0].text`, com `type: "text"` e `result.isError: !ok`. Não usa erro JSON-RPC para falha de execução da ingestão; erros de protocolo e ferramentas indisponíveis continuam usando o mecanismo existente. Readers não expõem a ferramenta mutável.

## Seleção e contagem

`scan_complete` indica que a varredura terminou. `total` conta somente `.md`, `.txt` e `.json` não excluídos pela política. Extensões desconhecidas e arquivos/diretórios ignorados não entram no total. Falha durante a varredura impede iniciar o lote e retorna seleção incompleta, sem declarar a pasta vazia.

Cada arquivo selecionado tem exatamente um resultado, ordenado por path relativo, mesmo com workers concorrentes. Depois de cancelamento, itens selecionados ainda não iniciados aparecem como `cancelled`. O relatório é desta chamada, não um registro persistente de jobs.

| Outcome / contador | Significado |
| --- | --- |
| `created` | Nova revisão de documento sem head anterior, publicada e sincronização concluída. |
| `updated` | Revisão de documento existente publicada ou head pendente reativado, com sincronização concluída. |
| `unchanged` | Revisões de documento e escopo continuam iguais e o head está ativo. |
| `skipped` | Documento de outro writer, ou excluído pela política entre seleção e execução. |
| `missing` | Fonte desapareceu entre seleção e execução; verificação de exclusão pendente concluída, sem remoção de vetores. Não implica que existia um head a marcar. |
| `failed` | Processamento desse arquivo falhou; os demais continuam. |
| `cancelled` | Operação cancelada ou com prazo expirado, inclusive antes de iniciar um item selecionado. |

`total = created + updated + unchanged + skipped + missing + failed + cancelled` após seleção completa. `ingested = created + updated` mantém o campo público com semântica corrigida. Não é contagem de unidades únicas ou detecção de duplicatas entre arquivos.

## Resultado de arquivo

- `path`: caminho relativo a `HIVE_DATA_DIR`. Nunca retorna destino absoluto de symlink. Omitido se não for possível construir uma referência interna segura.
- `outcome`: um dos valores da tabela.
- `published`: `true` quando a publicação/reativação foi confirmada nesta chamada. Pode ser verdadeiro com `outcome: failed` se a limpeza falhar depois do commit. `false` não prova ausência de escrita remota, pois a confirmação de um commit pode se perder.
- `reason_code` e `detail`: motivo estável e mensagem sanitizada, sem corpo do documento ou mensagem bruta do provedor; omitidos em sucesso comum.

Motivos atuais: `foreign_writer`, `ignored`, `cancelled`, `writer_required`, `missing_file`, `pending_delete_failed`, `infrastructure_failed`, `document_read_failed`, `scope_unavailable`, `invalid_metadata`, `invalid_document`, `empty_document`, `chunk_limit`, `file_size_limit`, `unsupported_format`, `invalid_encoding`, `control_unavailable`, `tombstoned`, `embedding_failed`, `staging_failed`, `verification_failed`, `commit_failed`, `cleanup_pending`, `synchronization_failed`.

Os motivos são atribuídos na etapa que falhou e não por busca de palavras em erros externos. `unsupported_format` está disponível na sincronização individual; a seleção de workspace atual exclui esse formato antes de processá-lo.

`cleanup_pending` informa que a nova revisão está ativa e a anterior pode permanecer fisicamente no índice, mas inativa para consultas. O relatório não implementa um novo mecanismo de limpeza retroativa. `commit_failed` exige reconciliação, sem presumir rollback.

## Compatibilidade e limites

Substitui o resumo textual da CLI e a resposta Markdown da ferramenta MCP. Consumidores devem ler `schema_version`, tratar outcomes desconhecidos conservadoramente e aceitar campos adicionais. Não há modificação de IDs, hashing, propriedade ou protocolo de publicação de documentos.

O relatório inclui todos os arquivos selecionados em memória e na resposta. Paginação, persistência de jobs e inventário de formatos desconhecidos fazem parte de etapas futuras do plano. Paths relativos ainda são metadados privados e devem ter o mesmo tratamento de acesso que os resultados atuais do writer.
