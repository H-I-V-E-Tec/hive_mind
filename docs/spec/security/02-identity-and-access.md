# Segurança 02 — Identidade e controle de acesso

## Objetivo

Garantir que somente os dois dispositivos e operadores autorizados consigam alcançar e consultar os dados do Hive.

## Requisitos da v0.1

- A rede privada mantém uma lista explícita de dispositivos autorizados e bloqueia os demais.
- Qdrant exige credencial exclusiva desta instalação, além do controle de rede.
- O processo MCP executa como usuário sem privilégios administrativos e lê apenas `HIVE_DATA_DIR` e sua configuração necessária.
- Cada processo fica preso ao `HIVE_ID` e `HIVE_COLLECTION` configurados; o cliente MCP não pode substituí-los em uma chamada.
- A configuração e os arquivos locais usam permissões mínimas do sistema operacional.

## Admissão e revogação

Adicionar um dispositivo exige aprovação do operador, identidade na VPN e instalação segura da credencial. Ao remover um dispositivo: revogar sua identidade de rede, rotacionar a chave do Qdrant, atualizar os dispositivos restantes e verificar logs desde o último acesso conhecido.

## Limite conhecido

A conexão direta ao Qdrant com chave compartilhada não identifica usuários individualmente. Até existir gateway com identidade própria, não se deve declarar RBAC por usuário, atribuição individual de consulta ou acesso diferente por programa.

## Aceite e testes

- Chamadas MCP não conseguem alterar Hive ou collection.
- Credencial ausente ou inválida nega acesso sem fallback anônimo.
- Existe runbook testado de admissão, revogação e rotação.
- Testes cobrem negação por padrão e isolamento de configuração.
