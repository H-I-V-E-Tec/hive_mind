# Segurança 09 — Backup, recuperação e incidentes

## Objetivo

Recuperar dados íntegros e responder rapidamente a perda, vazamento ou acesso indevido.

## Backup e restauração

- Definir RPO, RTO e retenções como valores numéricos no [perfil operacional](../../operations/deployment-profile.md) antes do uso real; valor ausente reprova o gate de release.
- Backups são criptografados, autenticados, versionados, retidos pelo prazo definido e acessíveis somente aos operadores autorizados.
- Procedimento de restauração verifica integridade, collections de dados e controle, `hive_id`, revisões/tombstones, aprovações, metadados e fingerprint de embedding/schema.
- Teste periódico prova que o backup pode ser restaurado; existência do arquivo não é evidência suficiente.

## Incidentes

O runbook cobre perda de dispositivo, chave exposta, certificado comprometido, Qdrant acessível publicamente, dado misturado e dependência vulnerável. A sequência padrão é conter acesso, revogar/rotacionar, preservar evidência mínima, avaliar exposição, restaurar e registrar ações.

## Aceite

- Rotação e restauração são ensaiadas antes da v0.1.
- Responsáveis e canais de acionamento estão definidos fora de dados sensíveis.
- O teste registra data, resultado, RPO/RTO alvo e observado, retenção aplicada e correções necessárias.
