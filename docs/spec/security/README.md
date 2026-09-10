# Specs de segurança

Segurança é requisito de aceite do Hive Mind, conforme a [decisão 002](../../decisions/002-security-is-primary.md). Estas specs são transversais: devem ser aplicadas junto às specs funcionais, desde a primeira implementação.

## Modelo de segurança da v0.1

A v0.1 atende uma única equipe e dois dispositivos confiáveis. A unidade de acesso é o dispositivo autorizado pela rede privada e pela credencial exclusiva do Hive. Não há alegação de RBAC por usuário na conexão direta ao Qdrant. Revogar um dispositivo exige removê-lo da rede privada e rotacionar a credencial compartilhada. Controle granular por usuário ou programa exigirá uma camada autenticada própria em uma fase posterior.

| Spec | Controle principal |
| --- | --- |
| [01](01-threat-model.md) | Ameaças, ativos e limites de confiança. |
| [02](02-identity-and-access.md) | Admissão, privilégio mínimo e revogação. |
| [03](03-network-and-cryptography.md) | Criptografia em trânsito e superfície de rede. |
| [04](04-secrets-management.md) | Ciclo de vida de credenciais e prevenção de vazamentos. |
| [05](05-data-protection-and-isolation.md) | Classificação, isolamento, retenção e dados em repouso. |
| [06](06-secure-ingestion-and-retrieval.md) | Arquivos não confiáveis, integridade e respostas MCP. |
| [07](07-audit-and-privacy.md) | Auditoria útil com conteúdo mínimo. |
| [08](08-supply-chain-and-runtime.md) | Dependências, build e execução endurecidos. |
| [09](09-backup-recovery-and-incidents.md) | Backup, restauração, revogação e resposta. |
| [10](10-security-verification.md) | Gates para liberar a v0.1. |

## Regras comuns

- Negar por padrão quando identidade, escopo, transporte ou configuração não puderem ser validados.
- Nunca registrar segredos ou conteúdo integral de recon em logs e diagnósticos.
- Todo controle deve ter teste automatizado quando possível e procedimento de verificação operacional quando depender da infraestrutura.
- Exceções precisam de uma decisão registrada com responsável, prazo e risco aceito.
