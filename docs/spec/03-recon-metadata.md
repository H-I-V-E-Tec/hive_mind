# 03 — Metadados de recon

## Resultado

Todo chunk pode ser isolado e interpretado por metadados confiáveis de programa, escopo e proveniência.

## Contrato de payload

| Campo | Regra |
| --- | --- |
| `hive_id`, `program_id`, `document_type`, `scope_status`, `classification`, `source`, `collected_at`, `tags`, `path` | obrigatórios em todos os pontos Hive. |
| `document_type` | `scope`, `rules`, `asset`, `endpoint`, `note`, `evidence` ou `unknown`. |
| `scope_status` | `authorized`, `out_of_scope` ou `unknown`. |
| `classification` | `internal`, `restricted` ou `unknown`. |

Front matter e cabeçalho Markdown têm precedência. Na ausência deles, `program_id` e tipo são inferidos do caminho `programs/<program_id>/...`; valores não inferíveis são `unknown`. Tags são normalizadas e datas são RFC 3339.

Criar índices de payload para todos os campos filtráveis. A criação e reconciliação de índices deve ser idempotente.

## Segurança

Metadados inválidos não podem ser convertidos silenciosamente em `authorized`. A ausência de confirmação deve permanecer `unknown`.

## Aceite e testes

- Extração, inferência, precedência e normalização são testadas.
- Todos os chunks de um arquivo compartilham seus metadados.
- Testes verificam índices e que escopo inválido nunca ganha privilégio.
