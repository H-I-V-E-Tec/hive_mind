# 005 — Concessões de programa no servidor Mind

- Estado: aceita
- Data: 2026-10-23
- Substitui: [004](004-scope-status-administrativo.md) para a autorização do reader de escopo

## Contexto

O Center agora emite concessões separadas de leitura e contribuição por programa em JWT versionado. A variante administrativa da ADR 004 não serve à identidade reader necessária para o hive_burp. O Mind precisa aplicar a concessão antes de consultar dados ou publicar conteúdo.

## Decisão

Todas as rotas HTTP remotas com `program_id` exigem claim v1 válida e concessão correspondente, além de permissões globais. `scope status` aceita reader com leitura do programa e permanece somente leitura. Publicação de manifesto e aprovação exigem contribuição no programa mais `mind.scope.approve`. O catálogo global considera somente programas concedidos antes de contagem/ranking/paginação; lista vazia produz catálogo vazio. `get_sync_status` informa apenas disponibilidade ao reader, sem contadores globais. Tokens legados sem versão não dão acesso por programa. O CLI local operacional mantém seu modelo de dispositivo e não usa JWT.

## Consequências

A API do Center precisa estar migrada e os membros precisam receber concessões antes do deploy deste servidor; sem isso, rotas remotas por programa falham fechadas. A janela de revogação de token permanece de até 12 horas conforme a spec 026 do Center, impedindo promessa de revogação imediata. A ADR 004 continua documentando o primeiro reader administrativo e o hash de aprovação; somente sua escolha de autorização foi substituída.
