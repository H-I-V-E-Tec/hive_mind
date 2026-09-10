# 01 — Configuração exclusiva do Hive Mind

## Resultado

O binário aceita somente a configuração do Hive Mind e falha cedo quando ela estiver incompleta ou insegura.

## Contrato

`HIVE_ID`, `HIVE_DATA_DIR`, `HIVE_COLLECTION`, `OLLAMA_HOST` e `EMBEDDING_MODEL` são obrigatórios. `QDRANT_HOST`, `QDRANT_PORT`, `QDRANT_API_KEY` e parâmetros TLS configuram a conexão privada. Não haverá `HIVE_MODE`, `WATCH_DIRECTORY` ou `QDRANT_COLLECTION` legados.

Criar `.env.hive.example` sem segredos. Flags devem usar os mesmos nomes conceituais, e sua precedência deve ser: flags, ambiente, arquivo de configuração e padrão seguro.

## Segurança

Validar `HIVE_ID` e collection como identificadores seguros; rejeitar diretório inexistente, URLs sem esquema e configuração remota sem proteção de transporte aprovada. Não exibir variáveis sensíveis em erros.

## Aceite e testes

- Configuração completa produz valores efetivos previsíveis.
- Ausências e valores inválidos falham antes de abrir watcher ou cliente.
- Não existe caminho de compatibilidade com as variáveis herdadas.
- Testes cobrem precedência, validação e mascaramento de segredos.
