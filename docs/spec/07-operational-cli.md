# 07 — Operação segura

## Resultado

O operador consegue validar e usar a instância sem inspeção manual de banco, rede ou código.

## Comandos

- `inventory <dir> [--program=<id>] [--details] [--hash-max-bytes=<n>]`: diagnóstico local de formatos, tamanhos e cópias integrais candidatas por SHA-256, conforme o [contrato de inventário v1](contracts/inventory-report.md). Opera sem configuração Hive ou serviços e não altera documentos/índice.

- `ingest`: indexa `HIVE_DATA_DIR` e, após a inicialização, retorna o [relatório JSON de ingestão v1](contracts/ingestion-report.md). Separa criações, atualizações, inalterados, ignorados, ausentes, falhas e cancelamentos; preserva resultados individuais em falha parcial. `ingest --prune` só executa a poda quando a sincronização termina sem erro.
- `search`: chama a busca Hive com `program_id` e filtros por flags.
- `status`: mostra configuração efetiva mascarada, collection, modelo, dimensão, última sincronização e pendências no CLI operacional. No endpoint remoto `get_sync_status`, leitores veem apenas disponibilidade sem contadores globais; `mind.scope.approve` permite contadores operacionais da instância.
- `validate`: verifica papel, diretório quando aplicável, Qdrant, permissão da credencial, TLS, Ollama e fingerprint completo de embedding/schema.
- `scope approve <program_id>`: valida o manifesto, mostra somente resumo/hash, rematerializa o programa sob o novo `scope_revision` e registra aprovação após confirmação explícita. No CLI local, qualquer writer registrado pode aprovar e passa a ser o dono da aprovação. No MCP remoto, `hive_preview_scope_approval` mostra hash e contagens e `hive_approve_scope` confirma o hash exato, com `product.mind`, `mind.scope.approve` e contribuição no programa verificados pelo servidor writer. A rematerialização preserva o dono de cada documento.
- `scope status <program_id> --json`: consulta remota somente leitura e imprime exatamente o contrato [scope-status-v1](contracts/scope-status-v1.md) em stdout, sem log ou segredo. O servidor exige login, `product.mind` e concessão de leitura do programa no [contrato program-access-v1](contracts/program-access.md), reconstrói apenas o documento de escopo da revisão aprovada e revalida a aprovação antes de responder. Sem aprovação, responde `unapproved` com regras vazias; inconsistência, revisão/documento ausente e saída acima de 1 MiB falham fechados. Tokens legados sem versão de concessões não autorizam leitura de programa.
- `remove <path>`: registra tombstone e remove de forma verificável a revisão ativa; disponível apenas ao writer dono do documento (código `12` para documento de outro writer).
- `audit record <evento> <change>`: registra rotação, revogação ou promoção executada pelo operador.
- `audit report [--since=AAAA-MM-DD] [--program=<id>]`: agrega os eventos de recuperação dos logs locais conforme a [spec 09](09-usage-metrics.md); saída somente numérica.

Exit codes são estáveis: `0` sucesso, `2` uso/entrada inválida, `10` configuração, `11` conectividade, `12` autenticação/autorização, `13` TLS, `14` incompatibilidade de embedding/schema e `15` falha parcial recuperável. Quando houver mais de uma falha, prevalece o menor código não zero e todas as falhas sanitizadas aparecem no relatório estruturado.

No lote de ingestão da CLI, falhas individuais são resultados de processamento e produzem código agregado `15`, com motivos por arquivo. Cancelamento também produz `15`; erros de infraestrutura anteriores ao lote preservam sua classificação operacional. `ingested` passa a representar somente `created + updated`, sem contar arquivos inalterados. A varredura inteira fica restrita à CLI operacional; o MCP remoto publica um documento por chamada e preserva o mesmo relatório por item, com `isError: true` em falhas.

## Segurança

`status` e `validate` não revelam chaves, tokens, conteúdo indexado nem detalhes internos de rede além do necessário para o operador. A configuração `HIVE_MAX_CLASSIFICATION` (`internal` por padrão, opcionalmente `restricted`) é imposta pelo servidor e não pode ser elevada pelo cliente; classificação `unknown` não é retornada até ser corrigida. Essa política minimiza respostas MCP, mas não é apresentada como RBAC do banco.

## Aceite e testes

- Comandos que acessam o Hive exigem a nova configuração; `help` e `inventory` funcionam sem credenciais ou serviços.
- Readers recebem erro de autorização nos comandos mutáveis e não expõem ferramentas MCP mutáveis.
- Writers recebem erro de autorização ao remover documento de outro writer; `ingest` conclui com sucesso ignorando documentos alheios.
- Mocks cobrem saudável, indisponível, não autenticado, permissão insuficiente, TLS inválido e fingerprint incompatível.
- Ajuda, README e exemplos MCP refletem o contrato final.
- `scope status` entrega JSON compatível com `scope-status-v1` para manifesto aprovado, não executa escrita e recusa token sem concessão de leitura, programa divergente, documento incompleto e aprovação revogada.
- Testes de relatório cobrem múltiplas falhas, concorrência, idempotência, cancelamento, limpeza posterior à publicação e preservação do resumo em falha de prune, sem exposição de texto de documentos ou erros brutos de provedores.
