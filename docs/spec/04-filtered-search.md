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
  "scope_status": "authorized",
  "limit": 8
}
```

`query` e `program_id` são obrigatórios. O servidor sempre acrescenta o `hive_id` configurado. Tipos, tags, classificação e escopo são filtros opcionais validados. A resposta traz trecho limitado, `path`, fonte, tipo e escopo.

## Segurança

O servidor não aceita `hive_id` vindo do cliente. Filtros devem ser aplicados no Qdrant, antes de retornar conteúdo. Entradas inválidas retornam MCP `-32602` sem detalhes internos.

## Aceite e testes

- O schema MCP e os filtros enviados ao Qdrant correspondem ao contrato.
- Busca não retorna pontos de outro `hive_id` ou programa.
- Limite máximo e validação de enums são aplicados e testados.
