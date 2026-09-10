# 04 — Busca filtrada e isolada

## Resultado

`hive_search` fornece busca semântica com filtros nativos do Qdrant e nunca cruza dados de outro Hive ou programa sem solicitação explícita autorizada.

## Contrato MCP

```json
{
  "query": "hosts autorizados com OAuth e upload",
  "program_id": "acme-bugbounty",
  "document_types": ["asset", "endpoint", "scope"],
  "tags": ["oauth"],
  "effective_scope_status": "authorized",
  "limit": 8
}
```

`query` e `program_id` são obrigatórios. `query` tem de 1 a 2.000 caracteres. `limit` tem padrão 8 e intervalo de 1 a 20. O servidor sempre acrescenta `record_type=chunk`, o `hive_id` configurado, o programa e o limite máximo de classificação configurado. Tipos, tags, classificação e escopo efetivo são filtros opcionais validados. Tags usam semântica `ALL`; uma evolução futura poderá expor outro operador explicitamente.

A busca lê a aprovação ativa do programa, inclui seu `scope_revision` no filtro Qdrant e solicita candidatos adicionais para tolerar revisões inativas. Só devolve chunks cuja combinação `document_revision`/`scope_revision` corresponda ao registro ativo na collection de controle. Resultados são ordenados por score decrescente e, em empate, por `path` e ordinal. Duplicatas usam `document_id` e intervalo do chunk, nunca similaridade textual aproximada.

A resposta MCP é JSON estruturado com `results`, `warnings` e `truncated`. Cada resultado contém `text`, `score`, `path`, `source`, `collected_at`, `document_type`, `effective_scope_status`, `classification` e `untrusted_content: true`. Paths são relativos e texto recuperado nunca é interpolado em instruções da ferramenta.

## Segurança

O servidor não aceita `hive_id`, collection, revisão ou elevação de classificação vindos do cliente. Filtros de isolamento são aplicados no Qdrant antes de recuperar conteúdo; a validação da revisão ativa ocorre antes de retorná-lo. Entradas inválidas retornam MCP `-32602` sem detalhes internos.

## Aceite e testes

- O schema MCP e os filtros enviados ao Qdrant correspondem ao contrato.
- Busca não retorna pontos de outro `hive_id` ou programa.
- Revisão inativa, incompleta ou tombstonada não é retornada.
- Limites, ordenação, semântica de tags, classificação e validação de enums são aplicados e testados.
