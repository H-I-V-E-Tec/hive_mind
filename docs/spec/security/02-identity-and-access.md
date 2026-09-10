# Segurança 02 — Identidade e controle de acesso

## Objetivo

Garantir que somente os dois dispositivos e operadores autorizados consigam alcançar e consultar os dados do Hive.

## Requisitos da v0.1

- A rede privada mantém uma lista explícita de dispositivos autorizados e bloqueia os demais.
- Qdrant exige credencial distinta por dispositivo e papel, além do controle de rede. Writer recebe read-write; readers recebem read-only; ambos ficam limitados às collections do Hive.
- O processo MCP executa como usuário sem privilégios administrativos e lê apenas `HIVE_DATA_DIR` e sua configuração necessária.
- Cada processo fica preso ao `HIVE_ID`, `HIVE_DEVICE_ID`, `HIVE_ROLE`, `HIVE_COLLECTION` e collection de controle derivados da configuração; o cliente MCP não pode substituí-los em uma chamada.
- A configuração e os arquivos locais usam permissões mínimas do sistema operacional.

## Admissão e revogação

Adicionar um dispositivo exige aprovação do operador, identidade na VPN e instalação segura de credencial própria. Ao remover um dispositivo: revogar sua identidade de rede e credencial, rotacionar somente segredos que possam ter sido compartilhados ou expostos e verificar logs desde o último acesso conhecido. Promover um novo writer exige antes desativar ou revogar o anterior.

## Limite conhecido

A conexão direta ao Qdrant identifica no máximo a credencial/dispositivo, não a pessoa que operou o processo. Os tokens limitam collections e modo read/read-write, mas não filtram payload por programa. Até existir gateway com identidade própria, não se deve declarar RBAC por pessoa ou autorização diferente por programa.

## Aceite e testes

- Chamadas MCP não conseguem alterar Hive ou collection.
- Credencial ausente, inválida ou incompatível com o papel nega acesso sem fallback anônimo.
- Reader não consegue gravar nem alterar schema; writer não recebe a chave administrativa do Qdrant.
- Existe runbook testado de admissão, revogação e rotação.
- Testes cobrem negação por padrão e isolamento de configuração.
