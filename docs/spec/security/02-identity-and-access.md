# Segurança 02 — Identidade e controle de acesso

## Objetivo

Garantir que somente os dispositivos e operadores autorizados consigam alcançar, consultar e alimentar os dados do Hive.

## Requisitos da v0.1

- A rede privada mantém uma lista explícita de dispositivos autorizados e bloqueia os demais.
- Qdrant exige credencial distinta por dispositivo e papel, além do controle de rede. Cada writer recebe read-write; cada reader recebe read-only; todos ficam limitados às collections do Hive. Uma credencial nunca é compartilhada por duas pessoas, mesmo com o mesmo papel.
- Cada writer se registra com `HIVE_DEVICE_ID` e `HIVE_WRITER_APPROVAL_ID` próprios; a aplicação restringe cada writer aos documentos e aprovações de escopo que ele mesmo publicou (spec 00). Essa restrição é de aplicação: a credencial read-write do Qdrant permite tecnicamente escrever em qualquer ponto, por isso writers são pessoas de confiança da mesma equipe, não tenants.
- O processo MCP executa como usuário sem privilégios administrativos e lê apenas `HIVE_DATA_DIR` e sua configuração necessária.
- Cada processo fica preso ao `HIVE_ID`, `HIVE_DEVICE_ID`, `HIVE_ROLE`, `HIVE_COLLECTION` e collection de controle derivados da configuração; o cliente MCP não pode substituí-los em uma chamada.
- A configuração e os arquivos locais usam permissões mínimas do sistema operacional.

## Admissão e revogação

Adicionar um dispositivo exige aprovação do operador, identidade na VPN e instalação segura de credencial própria. Ao remover um dispositivo: revogar sua identidade de rede e credencial, rotacionar somente segredos que possam ter sido compartilhados ou expostos e verificar logs desde o último acesso conhecido. Adicionar um writer exige um change/ticket próprio (`HIVE_WRITER_APPROVAL_ID`) e credencial read-write individual; remover um writer exige revogar sua credencial, registrar `audit record credential_revocation` e decidir o destino dos documentos que ele possui (manter, ou tombstone pelo operador com a credencial revogada substituída).

## Limite conhecido

A conexão direta ao Qdrant identifica no máximo a credencial/dispositivo, não a pessoa que operou o processo. Os tokens limitam collections e modo read/read-write, mas não filtram payload por programa. Até existir gateway com identidade própria, não se deve declarar RBAC por pessoa ou autorização diferente por programa.

## Aceite e testes

- Chamadas MCP não conseguem alterar Hive ou collection.
- Credencial ausente, inválida ou incompatível com o papel nega acesso sem fallback anônimo.
- Reader não consegue gravar nem alterar schema; writer não recebe a chave administrativa do Qdrant.
- Um writer não consegue, pela aplicação, alterar ou remover documento de outro writer nem invalidar aprovação de escopo que não fez.
- Existe runbook testado de admissão, revogação e rotação.
- Testes cobrem negação por padrão e isolamento de configuração.
