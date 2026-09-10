# Segurança 09 — Backup, recuperação e incidentes

## Objetivo

Recuperar dados íntegros e responder rapidamente a perda, vazamento ou acesso indevido.

## Backup e restauração

- Definir RPO e RTO antes do uso real.
- Backups são criptografados, autenticados, versionados, retidos pelo prazo definido e acessíveis somente aos operadores autorizados.
- Procedimento de restauração verifica integridade, collection, `hive_id`, metadados e compatibilidade de embedding.
- Teste periódico prova que o backup pode ser restaurado; existência do arquivo não é evidência suficiente.

## Incidentes

O runbook cobre perda de dispositivo, chave exposta, certificado comprometido, Qdrant acessível publicamente, dado misturado e dependência vulnerável. A sequência padrão é conter acesso, revogar/rotacionar, preservar evidência mínima, avaliar exposição, restaurar e registrar ações.

## Aceite

- Rotação e restauração são ensaiadas antes da v0.1.
- Responsáveis e canais de acionamento estão definidos fora de dados sensíveis.
- O teste registra data, resultado, RPO/RTO observado e correções necessárias.
