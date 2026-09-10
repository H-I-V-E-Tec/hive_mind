# Segurança 07 — Auditoria e privacidade

## Objetivo

Registrar ações suficientes para investigar acesso e alterações sem criar uma segunda cópia sensível dos dados.

## Eventos mínimos

- início/fim, versão e identificador do dispositivo/processo;
- validação de configuração, autenticação e TLS, sem valores secretos;
- ingestão, atualização e remoção por `hive_id`, `program_id`, hash e path relativo;
- consulta por ferramenta, filtros, quantidade e duração, sem pergunta ou trechos por padrão;
- falhas de autorização, integridade e configuração;
- rotação/revogação de credencial e restauração de backup.

## Requisitos

Logs são locais, estruturados, protegidos por permissão e rotação. Timestamps usam UTC. Definir retenção e procedimento de exclusão. Logs não contêm conteúdo de documento, embedding, consulta integral, chave, token ou path absoluto.

## Aceite e testes

- Testes com dados sentinela confirmam que conteúdo e segredos não aparecem.
- Eventos possuem correlação suficiente para reconstruir operação e origem da mudança.
- Falha ao escrever auditoria essencial deve ser visível; ações sensíveis definidas pelo modelo de ameaças falham fechado.
