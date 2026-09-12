# Segurança 07 — Auditoria e privacidade

## Objetivo

Registrar ações suficientes para investigar acesso e alterações sem criar uma segunda cópia sensível dos dados.

## Eventos mínimos

- início/fim, versão, `HIVE_DEVICE_ID`, papel e identificador do processo;
- validação de configuração, autenticação e TLS, sem valores secretos;
- ingestão, commit de revisão, atualização, tombstone, remoção e aprovação/invalidação de escopo por `hive_id`, `program_id`, hash e path relativo;
- consulta por ferramenta, filtros, quantidade, duração, tamanho da resposta serializada, tamanho dos documentos de origem e truncamento (spec 09), sem pergunta ou trechos por padrão;
- documento ignorado por pertencer a outro writer (`document_skipped`/`foreign_writer`);
- falhas de autorização, integridade e configuração;
- rotação/revogação de credencial e restauração de backup.

## Requisitos

Logs são locais, estruturados, protegidos por permissão e rotação. Timestamps usam UTC. Definir retenção e procedimento de exclusão. Logs não contêm conteúdo de documento, embedding, consulta integral, chave, token ou path absoluto.

## Aceite e testes

- Testes com dados sentinela confirmam que conteúdo e segredos não aparecem, inclusive no relatório `audit report`.
- Eventos possuem correlação suficiente para reconstruir operação e origem da mudança.
- Falha ao auditar aprovação de escopo, commit/tombstone, mudança de papel/credencial ou restauração falha fechado. Falha em evento apenas diagnóstico fica visível no `status` e não é ocultada.
