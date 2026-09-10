# 06 — Qdrant privado e autenticado

## Resultado

O Qdrant compartilhado só é acessível a participantes autorizados, autentica cada cliente e protege o tráfego.

Esta etapa implementa os requisitos detalhados nas specs de [identidade e acesso](security/02-identity-and-access.md), [rede e criptografia](security/03-network-and-cryptography.md) e [segredos](security/04-secrets-management.md).

## Comportamento

Adicionar `QDRANT_API_KEY`, `QDRANT_USE_TLS` e `QDRANT_TLS_SERVER_NAME`. O cliente encaminha autenticação e verifica TLS. A configuração Docker de referência deve ter volume persistente, API key via ambiente e portas vinculadas apenas a loopback ou interface VPN configurada, nunca a todas as interfaces por padrão.

## Segurança

MCP continua local via `stdio`; somente Qdrant é compartilhado. TLS é obrigatório fora de loopback, exceto exceção aprovada e registrada para rede privada controlada. Segredos não entram no repositório, logs, erros, tool responses ou `status`.

## Aceite e testes

- Testes confirmam o encaminhamento de API key/TLS e o mascaramento de segredos.
- `validate` bloqueia configuração remota insegura.
- Compose e documentação não expõem Qdrant publicamente.
