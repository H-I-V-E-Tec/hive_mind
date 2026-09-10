# 01 — Configuração exclusiva do Hive Mind

## Resultado

O binário aceita somente a configuração do Hive Mind e falha cedo quando ela estiver incompleta ou insegura.

## Contrato

`HIVE_ID`, `HIVE_DEVICE_ID`, `HIVE_ROLE`, `HIVE_COLLECTION`, `QDRANT_URL`, `QDRANT_API_KEY`, `OLLAMA_URL` e `EMBEDDING_MODEL` são obrigatórios. `HIVE_DATA_DIR` é obrigatório para o writer e opcional, somente leitura, para readers. `HIVE_ROLE` aceita apenas `writer` ou `reader`. Não haverá `HIVE_MODE`, `WATCH_DIRECTORY`, `QDRANT_COLLECTION`, `QDRANT_HOST` ou `QDRANT_PORT` legados.

Criar `.env.hive.example` sem segredos. O arquivo de configuração, quando usado, é TOML simples, com chaves na raiz e valores string, e somente é lido quando indicado por `--config`; não há descoberta automática. Chaves desconhecidas, repetidas ou seções são rejeitadas. Flags não sensíveis usam os mesmos nomes conceituais, e sua precedência é: flags, ambiente, arquivo indicado e padrão seguro. Segredos nunca são aceitos como flags. Valores obrigatórios não possuem padrão.

`QDRANT_URL` e `OLLAMA_URL` são URLs absolutas com esquema. `QDRANT_TLS_CA_FILE` permite confiar em uma CA privada e `QDRANT_TLS_SERVER_NAME` só substitui o hostname esperado quando configurado explicitamente. Não existe opção equivalente a `insecure-skip-verify`.

## Segurança

Validar Hive/device/program IDs com `[a-z0-9][a-z0-9_-]{0,63}` e `HIVE_COLLECTION` com `[a-z0-9][a-z0-9_-]{0,54}`, reservando espaço para o sufixo `__control`; canonicalizar e verificar o diretório; rejeitar URLs sem esquema, API key ausente e Qdrant fora de loopback sem TLS. A capacidade read-write/read-only da credencial é verificada durante a inicialização segura definida na spec 06, ainda antes de watcher ou MCP. Não exibir variáveis sensíveis em erros. Arquivos de configuração com segredos devem ter permissões restritas e nunca podem estar dentro de `HIVE_DATA_DIR`.

## Aceite e testes

- Configuração completa produz valores efetivos previsíveis.
- Ausências e valores inválidos falham antes de abrir watcher ou cliente.
- Não existe caminho de compatibilidade com as variáveis herdadas.
- Reader não exige diretório e não recebe ferramentas de mutação; writer exige diretório legível. A spec 06 testa as permissões efetivas das credenciais.
- Testes cobrem precedência, arquivo explícito, validação de URL/IDs/papel, permissões e mascaramento de segredos.
