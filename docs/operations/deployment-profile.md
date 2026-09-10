# Perfil operacional da implantação v0.1

Este arquivo é um template. Antes de usar dados reais, substitua todos os valores `UNSET`; qualquer campo não definido reprova o gate de release.

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
| Writer autorizado | `UNSET HIVE_DEVICE_ID` | Um único dispositivo com token read-write. |
| Reader autorizado | `UNSET HIVE_DEVICE_ID` | Dispositivo com token read-only. |

Exceções devem apontar para uma decisão contendo responsável, risco, compensação e prazo. Este arquivo nunca contém chaves, tokens, endereços pessoais ou conteúdo de recon.
