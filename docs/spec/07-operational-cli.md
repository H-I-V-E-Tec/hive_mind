# 07 — Operação segura

## Resultado

O operador consegue validar e usar a instância sem inspeção manual de banco, rede ou código.

## Comandos

- `ingest`: indexa `HIVE_DATA_DIR` e mostra totais sem conteúdo sensível.
- `search`: chama a busca Hive com `program_id` e filtros por flags.
- `status`: mostra configuração efetiva mascarada, collection, modelo, dimensão, última sincronização e pendências.
- `validate`: verifica diretório, Qdrant, autenticação, TLS, Ollama, modelo e compatibilidade da dimensão.

Cada falha possui código de saída distinto para configuração, conectividade, autenticação/TLS e incompatibilidade de embedding.

## Segurança

`status` e `validate` não revelam chaves, tokens, conteúdo indexado nem detalhes internos de rede além do necessário para o operador.

## Aceite e testes

- Todos os comandos funcionam apenas com a nova configuração Hive.
- Mocks cobrem saudável, indisponível, não autenticado, TLS inválido e dimensão incompatível.
- Ajuda, README e exemplos MCP refletem o contrato final.
