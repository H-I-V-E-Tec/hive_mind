# 08 — Aceitação da v0.1

## Resultado

Uma nota criada em uma máquina autorizada é recuperada nas outras com escopo, proveniência e acesso protegidos, e várias pessoas escrevem no mesmo Hive sem interferir umas nas outras.

## Cenário

Preparar ao menos duas máquinas writer e uma reader com a mesma versão e fingerprint de embeddings, credenciais de menor privilégio distintas por dispositivo, um Qdrant exclusivo acessível por VPN ou rede privada controlada, TLS/autenticação ativos e uma pasta Hive por writer (sincronizada privadamente ou independente). Criar e aprovar `scope.json`, além de nota e evidência de um programa explicitamente autorizado. O [ensaio de quatro pessoas](../operations/four-person-trial.md) é a execução de referência deste cenário e produz a evidência da [spec 09](09-usage-metrics.md).

1. Executar `validate` em todas as máquinas e confirmar papéis, registro de cada writer, permissões e fingerprint completo.
2. Indexar a nota no Computador A.
3. Buscar no Computador B por programa e tag, pelo MCP e CLI.
3a. Indexar no Computador B um documento com o mesmo path do Computador A e confirmar que ele é ignorado, que a revisão de A permanece e que `remove` em B é negado; indexar em B um documento próprio e recuperá-lo em A.
3b. Sem `scope.json` na pasta de B, ingerir em B uma nota do programa aprovado por A e confirmar escopo efetivo `authorized` e aprovação intacta.
4. Executar `hive_get_context` com um ativo explícito e confirmar que o escopo vem antes da evidência.
5. Consultar ativo sem escopo confirmado e confirmar o aviso.
6. Inserir documento comum que declare `authorized` e confirmar que ele não concede autorização.
7. Alterar a nota e confirmar que somente a nova revisão é retornada; simular falha antes do commit e confirmar que a revisão anterior permanece ativa.
8. Confirmar que o reader não consegue ingerir, aprovar escopo, remover documento ou alterar schema.
9. Remover a nota pelo fluxo com tombstone e confirmar que ela não é retornada.

## Critérios de saída

- A busca apresenta trecho curto, caminho e fonte corretos.
- Nenhuma consulta cruza `hive_id` ou programa.
- Apenas manifesto aprovado concede autorização e toda resposta recuperada marca conteúdo não confiável.
- Não há MCP público, Qdrant público, chave versionada ou tráfego remoto inseguro.
- Todos os gates da [validação de segurança](security/10-security-verification.md) estão aprovados.
- Todos os testes automatizados passam e um registro sanitizado da execução é armazenado em `docs/`.
- `audit report` de cada dispositivo foi coletado e consolidado conforme a spec 09.
