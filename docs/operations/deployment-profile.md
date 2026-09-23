# Perfil operacional da implantação v0.1

Este arquivo é um template operacional, não um requisito de publicação. Para usar a verificação auditada opcional, substitua todos os valores `UNSET`; campos não definidos fazem essa verificação falhar, mas não bloqueiam a release por tag. Defina os parâmetros reais antes de operar uma implantação de produção.

| Parâmetro | Valor | Evidência esperada |
| --- | --- | --- |
| Responsável operacional | `UNSET` | Nome ou função acionável. |
| Canal de incidente | `UNSET` | Canal protegido, sem segredos neste arquivo. |
| RPO máximo | `UNSET horas` | Frequência de backup compatível. |
| RTO máximo | `UNSET horas` | Ensaio de restauração dentro do prazo. |
| Retenção dos documentos | `UNSET dias` | Política da pasta canônica. |
| Retenção dos vetores/tombstones | `UNSET dias` | Job de reconciliação e exclusão. |
| Retenção dos backups | `UNSET dias` | Expiração verificável no repositório de backup. |
| Retenção dos logs | `UNSET dias` | Rotação e exclusão local verificadas. |
| Carência para `ingest --prune` | `UNSET horas` | Maior que o atraso máximo esperado da sincronização. |
| Frequência do teste de restauração | `UNSET dias` | Relatório sanitizado da última execução. |
| Writers autorizados | `UNSET HIVE_DEVICE_ID, ...` | Um `HIVE_DEVICE_ID` e um `HIVE_WRITER_APPROVAL_ID` por pessoa, cada um com token read-write individual. |
| Readers autorizados | `UNSET HIVE_DEVICE_ID, ...` | Dispositivos com token read-only individual. |

Exceções devem apontar para uma decisão contendo responsável, risco, compensação e prazo. Este arquivo nunca contém chaves, tokens, endereços pessoais ou conteúdo de recon.
