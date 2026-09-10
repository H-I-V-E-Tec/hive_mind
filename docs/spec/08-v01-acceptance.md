# 08 — Aceitação da v0.1

## Resultado

Uma nota criada em uma máquina autorizada é recuperada na outra com escopo, proveniência e acesso protegidos.

## Cenário

Preparar duas máquinas com mesma versão/modelo de embeddings, um Qdrant exclusivo acessível por VPN ou rede privada controlada, TLS/autenticação ativos e uma pasta Hive sincronizada privadamente. Criar `scope.md`, nota e evidência de um programa explicitamente autorizado.

1. Executar `validate` nas duas máquinas e confirmar mesma dimensão.
2. Indexar a nota no Computador A.
3. Buscar no Computador B por programa e tag, pelo MCP e CLI.
4. Executar `hive_get_context` e confirmar que o escopo vem antes da evidência.
5. Consultar ativo sem escopo confirmado e confirmar o aviso.
6. Alterar a nota e confirmar que o resultado antigo foi removido.

## Critérios de saída

- A busca apresenta trecho curto, caminho e fonte corretos.
- Nenhuma consulta cruza `hive_id` ou programa.
- Não há MCP público, Qdrant público, chave versionada ou tráfego remoto inseguro.
- Todos os gates da [validação de segurança](security/10-security-verification.md) estão aprovados.
- Todos os testes automatizados passam e um registro sanitizado da execução é armazenado em `docs/`.
