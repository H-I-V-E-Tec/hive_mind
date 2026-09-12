# 07 — Operação segura

## Resultado

O operador consegue validar e usar a instância sem inspeção manual de banco, rede ou código.

## Comandos

- `ingest`: indexa `HIVE_DATA_DIR` e mostra totais sem conteúdo sensível, separando documentos ingeridos de documentos ignorados por pertencerem a outro writer.
- `search`: chama a busca Hive com `program_id` e filtros por flags.
- `status`: mostra configuração efetiva mascarada, collection, modelo, dimensão, última sincronização e pendências.
- `validate`: verifica papel, diretório quando aplicável, Qdrant, permissão da credencial, TLS, Ollama e fingerprint completo de embedding/schema.
- `scope approve <program_id>`: valida o manifesto, mostra somente resumo/hash, rematerializa o programa sob o novo `scope_revision` e registra aprovação após confirmação explícita; disponível a qualquer writer registrado, que passa a ser o dono da aprovação, e nunca por MCP. A rematerialização preserva o dono de cada documento.
- `remove <path>`: registra tombstone e remove de forma verificável a revisão ativa; disponível apenas ao writer dono do documento (código `12` para documento de outro writer).
- `audit record <evento> <change>`: registra rotação, revogação ou promoção executada pelo operador.
- `audit report [--since=AAAA-MM-DD] [--program=<id>]`: agrega os eventos de recuperação dos logs locais conforme a [spec 09](09-usage-metrics.md); saída somente numérica.

Exit codes são estáveis: `0` sucesso, `2` uso/entrada inválida, `10` configuração, `11` conectividade, `12` autenticação/autorização, `13` TLS, `14` incompatibilidade de embedding/schema e `15` falha parcial recuperável. Quando houver mais de uma falha, prevalece o menor código não zero e todas as falhas sanitizadas aparecem no relatório estruturado.

## Segurança

`status` e `validate` não revelam chaves, tokens, conteúdo indexado nem detalhes internos de rede além do necessário para o operador. A configuração `HIVE_MAX_CLASSIFICATION` (`internal` por padrão, opcionalmente `restricted`) é imposta pelo servidor e não pode ser elevada pelo cliente; classificação `unknown` não é retornada até ser corrigida. Essa política minimiza respostas MCP, mas não é apresentada como RBAC do banco.

## Aceite e testes

- Todos os comandos funcionam apenas com a nova configuração Hive.
- Readers recebem erro de autorização nos comandos mutáveis e não expõem ferramentas MCP mutáveis.
- Writers recebem erro de autorização ao remover documento de outro writer; `ingest` conclui com sucesso ignorando documentos alheios.
- Mocks cobrem saudável, indisponível, não autenticado, permissão insuficiente, TLS inválido e fingerprint incompatível.
- Ajuda, README e exemplos MCP refletem o contrato final.
