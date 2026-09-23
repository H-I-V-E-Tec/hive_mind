# 06 — Qdrant privado e autenticado

## Resultado

O Qdrant compartilhado só é acessível a dispositivos autorizados, exige credenciais com privilégio compatível com o papel e protege o tráfego.

Esta etapa implementa os requisitos detalhados nas specs de [identidade e acesso](security/02-identity-and-access.md), [rede e criptografia](security/03-network-and-cryptography.md) e [segredos](security/04-secrets-management.md).

## Comportamento

Usar `QDRANT_URL`, `QDRANT_API_KEY`, `QDRANT_TLS_CA_FILE` e, quando necessário, `QDRANT_TLS_SERVER_NAME`. O cliente encaminha autenticação, exige TLS fora de loopback e valida cadeia, validade e hostname. A configuração Docker de referência deve fixar uma versão da imagem, usar volume persistente, receber chaves por secret/ambiente injetado e vincular portas apenas a loopback ou interface VPN configurada, nunca a todas as interfaces por padrão.

Cada writer usa um token read-write individual e cada reader um token read-only individual, todos limitados a `HIVE_COLLECTION` e `${HIVE_COLLECTION}__control`. Tokens nunca são compartilhados entre dispositivos, mesmo entre writers do mesmo Hive: a revogação de uma pessoa não pode afetar as demais. A chave administrativa do Qdrant não é entregue ao processo normal. Se a implantação escolhida não suportar tokens granulares, a limitação deve ser aplicada por instância exclusiva e a exceção registrada; não se deve alegar isolamento criptográfico entre collections.

## Segurança

MCP continua local via `stdio`; somente Qdrant é compartilhado. TLS é obrigatório fora de loopback. Uma exceção só pode seguir a decisão 002, ter prazo e risco registrados; sem aprovação explícita, o aceite operacional continua pendente, mesmo que o binário seja publicado. Segredos não entram no repositório, logs, erros, respostas de ferramenta ou `status`.

## Aceite e testes

- Testes confirmam API key, CA/hostname TLS, permissões de writer/reader e mascaramento de segredos.
- `validate` em cada writer confirma seu próprio registro e aprovação; em cada reader confirma leitura permitida e escrita negada nas duas collections.
- `validate` bloqueia configuração remota insegura.
- Compose e documentação não expõem Qdrant publicamente.
