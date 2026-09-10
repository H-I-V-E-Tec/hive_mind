# 03 — Metadados de recon

## Resultado

Todo chunk pode ser isolado e interpretado por metadados confiáveis de programa, escopo e proveniência.

## Contrato de payload

| Campo | Regra |
| --- | --- |
| `hive_id`, `program_id`, `record_type`, `document_type`, `claimed_scope_status`, `effective_scope_status`, `classification`, `source`, `collected_at`, `tags`, `asset_refs`, `path`, `document_id`, `document_revision`, `scope_revision`, `chunk_ordinal` | chaves obrigatórias em todos os chunks Hive. |
| `record_type` | sempre `chunk` na collection de dados. |
| `document_type` | `scope`, `rules`, `asset`, `endpoint`, `note`, `evidence` ou `unknown`. |
| `claimed_scope_status` | `authorized`, `out_of_scope` ou `unknown`; nunca concede autorização. |
| `effective_scope_status` | `authorized`, `out_of_scope` ou `unknown`; calculado somente do manifesto aprovado. |
| `classification` | `internal`, `restricted` ou `unknown`. |
| `source` | string não vazia ou `null`. |
| `collected_at` | RFC 3339 em UTC ou `null`. |
| `asset_refs` | lista possivelmente vazia de ativos normalizados pelo contrato de escopo. |

`program_id` é derivado exclusivamente do path `programs/<program_id>/...`; divergência com o conteúdo rejeita o arquivo. Arquivos fora dessa estrutura não são ingeridos na v0.1. O tipo é extraído do front matter e, na ausência, inferido de uma tabela explícita de paths; valor não inferível é `unknown`. Front matter pode fornecer classificação, fonte, data, tags, ativos referenciados e escopo declarado. Tags são minúsculas, sem duplicatas e ordenadas; datas são RFC 3339 normalizadas para UTC. `source` e `collected_at` ausentes são `null`, nunca string vazia ou data inventada.

O escopo efetivo é materializado para `asset_refs` a partir de `programs/<program_id>/scope.json`, validado pelo [schema versionado](contracts/scope-manifest.schema.json). Somente o hash aprovado pelo comando local `hive-mind scope approve <program_id>` pode produzir `authorized`; esse comando não é exposto por MCP. Sem ativo, todos os ativos autorizados ou aprovação vigente, o chunk fica `unknown`; se qualquer ativo estiver excluído, fica `out_of_scope`. Alteração ou remoção do manifesto invalida imediatamente a aprovação. O algoritmo de matching e normalização segue o [contrato do manifesto](contracts/scope-manifest.md).

Aprovar uma nova revisão de escopo rematerializa, sem recalcular embeddings, todos os chunks do programa sob o novo `scope_revision`; confirma o conjunto; e somente então ativa a aprovação na collection de controle. Até o commit, consultas continuam usando a aprovação anterior. Se o arquivo divergir do hash aprovado antes de uma nova aprovação, o programa falha fechado como `unapproved`/`unknown`.

Criar índices de payload para todos os campos filtráveis. A criação e reconciliação de índices deve ser idempotente.

## Segurança

Metadados inválidos não podem ser convertidos silenciosamente em `authorized`. Texto recuperado e `claimed_scope_status` nunca alimentam diretamente o escopo efetivo. A ausência de manifesto válido, aprovação vigente ou ativo identificável permanece `unknown`.

## Aceite e testes

- Extração, inferência, divergência e normalização são testadas.
- Todos os chunks de um arquivo compartilham seus metadados.
- Testes verificam índices, regras de inclusão/exclusão, invalidação por hash e que documento comum ou manifesto não aprovado nunca ganha privilégio.
