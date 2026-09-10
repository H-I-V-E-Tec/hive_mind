# 001 — Testes são obrigatórios

- Estado: Aceita
- Data: 2026-09-09

## Contexto

O Hive Mind manipula metadados de escopo, evidências e regras que precisam ser recuperados com precisão. Mudanças sem verificação podem misturar programas, remover contexto de escopo ou degradar buscas sem serem percebidas.

## Decisão

Todo comportamento novo ou alterado deve incluir testes automatizados adequados antes de ser considerado concluído. Correções de defeito devem incluir um teste que reproduza a falha quando isso for tecnicamente viável.

Os testes devem cobrir, conforme o componente, validação de configuração, extração de metadados, isolamento de filtros, composição de contexto, tratamento de credenciais e contratos MCP. `go test ./...` deve permanecer verde.

## Consequências

- Uma spec não é concluída somente com implementação manual demonstrada.
- Mocks de Qdrant e Ollama devem permitir testar cenários de erro, autenticação e dimensões incompatíveis.
- Testes de integração são exigidos para fluxos que dependem da interação real entre componentes; não devem ser substituídos por testes que apenas repetem a implementação.
