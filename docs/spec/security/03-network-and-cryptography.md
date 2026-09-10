# Segurança 03 — Rede e criptografia

## Objetivo

Proteger confidencialidade e integridade dos dados em trânsito e reduzir a superfície de rede ao mínimo.

## Requisitos

- MCP usa somente `stdio` local e não abre listener TCP/HTTP.
- Ollama permanece em loopback por padrão; acesso remoto a Ollama fica fora do escopo da v0.1.
- Qdrant fica acessível somente por loopback ou interface VPN/rede privada explicitamente configurada.
- TLS 1.2 ou superior protege toda conexão Qdrant que saia do host; certificado e nome do servidor são verificados. `insecure-skip-verify` não é permitido.
- Firewall nega entrada por padrão e libera somente a porta Qdrant para identidades/endereço da rede autorizada.
- Timeouts, limites de conexão e mensagens de erro não podem expor credenciais ou conteúdo.

## Criptografia

Usar bibliotecas do sistema/Go e suites modernas. Não implementar algoritmo criptográfico próprio. Certificados possuem validade monitorada e procedimento de renovação antes do vencimento.

## Aceite e testes

- Configuração remota sem TLS é rejeitada por `validate` e na inicialização.
- Certificado inválido, expirado ou com hostname incorreto falha fechado.
- Verificação operacional confirma que não há porta MCP/Ollama pública e que Qdrant não escuta em interface não autorizada.
