# Plano de melhoria arquitetural: permissões e ingestão pelo MCP remoto

Estado: perfis/permissões e ingestão remota implementados e validados localmente; publicação e aceite do deploy pendentes. Versão: 1.1.0. Data: 2026-10-06.

## Objetivo desta entrega

Permitir que uma pessoa autorizada envie uma nota pelo MCP remoto e a encontre na busca remota. Reutilizar o login, o JWT, a API HTTP e o pipeline de ingestão que já existem.

A integração continua sendo a atual: agente → `hive` por MCP `stdio` → serviço Mind por HTTPS. O conteúdo da nota atravessa essa conexão; um caminho do computador não é um arquivo no servidor.

Implementação deste corte: [`remote-ingestion.md`](docs/spec/contracts/remote-ingestion.md), `server/remote_ingestion.go` e `hive_ingest_document`. O teste integrado em containers descartáveis com Ollama e Qdrant reais confirmou envio pelo MCP, publicação, busca remota e replay sem novos embeddings/chunks. A API foi publicada na branch `feat/profile-product-permissions`, commit `fc86cf3`; o operador abre o PR e faz o deploy. O frontend continua sendo ajustado separadamente pelo operador.

A fila durável, novos serviços, persistência canônica PostgreSQL, refresh token, concessões detalhadas por programa e revisão editorial pertencem à evolução posterior. Este corte de teste não declara essas funcionalidades prontas nem substitui a [decisão de arquitetura 003](docs/decisions/003-canonical-persistence.md).

## Estrutura de permissões por perfil

A API mantém um catálogo de permissões com chave, grupo e dependências. O administrador escolhe as permissões de cada perfil e atribui um ou mais perfis a cada membro. O acesso efetivo é a união dessas concessões, válida apenas para membros ativos; permissões de ação sem seus pré-requisitos não ficam efetivas.

O portal deve carregar esse catálogo da API para mostrar acesso Mind, ingestão Mind e acesso Atlas, além das permissões de telas/administração. A inclusão de uma permissão no backend não pode depender de uma lista de checkboxes desatualizada no frontend. Todos os rótulos continuam nos arquivos de tradução do portal.

A migração inicial concede `mind.ingest` somente ao perfil de administrador. O perfil padrão de membro continua com leitura Mind. Perfis personalizados podem conceder a ingestão; a permissão só fica efetiva junto com `product.mind`, inclusive quando os dois direitos vêm de perfis diferentes.

O CRUD é dinâmico: o administrador cadastra nome, chave, descrição e permissões, edita depois ou exclui perfis personalizados. Criar perfis como leitor, contribuidor ou usuário Atlas não exige alteração de código. Os perfis e suas concessões são persistidos no banco; os perfis de sistema continuam protegidos.

| Operação | Rota da API existente |
| --- | --- |
| Consultar catálogo de permissões | `GET /api/v1/admin/permissions` |
| Listar perfis | `GET /api/v1/admin/access-profiles` |
| Criar perfil | `POST /api/v1/admin/access-profiles` |
| Editar perfil | `PUT /api/v1/admin/access-profiles/{id}` |
| Excluir perfil | `DELETE /api/v1/admin/access-profiles/{id}` |
| Atribuir perfis a um usuário | `PUT /api/v1/admin/members/{id}/access-profiles` |

Modelo existente: `access_profiles`, `access_profile_permissions`, `member_access_profiles` e `access_events`. Cada edição/atribuição registra autoria na auditoria; as rotas administrativas continuam exigindo permissão administrativa e proteção das mutações da sessão web.

Toda alteração de schema ou de dados iniciais obrigatórios deve ser versionada em migrations Alembic na `api-hive-center`. Atualizar instalações existentes com `alembic upgrade head`, preservando concessões e validando downgrade em banco descartável. Não atualizar o banco manualmente nem depender de seed no startup. Cadastrar, editar, excluir ou atribuir perfis pelo CRUD são operações normais nas tabelas existentes, com auditoria, e não exigem migrations por cadastro.

Contratos da implementação: [spec 012 da API](../api-hive-center/docs/specs/012-permissoes-dos-produtos.md) e [spec 013 do portal](../hive_center/docs/specs/013-permissoes-dos-produtos.md).

Validação local: 91 testes da API passaram; 9 testes de integração existentes dependem de seu banco específico e não foram executados nessa suíte. A migration 006 passou separadamente em PostgreSQL 16 descartável: upgrade, preservação das concessões existentes, atribuição somente ao administrador, downgrade e `alembic check`. No portal, 49 testes, typecheck, lint e build passaram. A migration e o código ainda precisam ser publicados e aplicados no ambiente implantado.

## 1. Permissões do usuário no JWT

O Center já emite JWT temporário com `permissions`, `profiles`, `aud`, `sub` e `exp`. Aproveitar isso, acrescentando apenas a permissão de ingestão ao catálogo e aos perfis escolhidos.

| Permissão | Efeito neste corte |
| --- | --- |
| `product.mind` | Acesso às consultas do Mind. |
| `mind.ingest` | Permissão adicional para enviar documentos ao Mind. Exige também `product.mind`. |
| `product.atlas` | Acesso ao Atlas, validado pelo serviço Atlas. Não concede acesso ou ingestão no Mind. |

Exemplo ilustrativo das claims de um contribuinte; valores de expiração são definidos pelo emissor:

```json
{
  "sub": "member-123",
  "aud": "mind",
  "permissions": ["product.mind", "mind.ingest"],
  "profiles": ["contribuidor"],
  "exp": 2000000000
}
```

Um leitor recebe `product.mind` sem `mind.ingest`. Não conceder ingestão automaticamente a todos os membros. O Center continua resolvendo as permissões dos perfis no login; o cliente não envia permissões no corpo do documento.

Mudanças de perfil passam a valer no próximo JWT emitido, mediante novo login ou expiração. Revogação imediata e renovação automática ficam para depois, conforme o recorte solicitado. Tokens de audiência `atlas` não são aceitos pela API Mind.

Arquivos de referência: [catálogo da API Center](../api-hive-center/src/hive_center_api/domain/permissions.py), [emissão de tokens](../api-hive-center/src/hive_center_api/adapters/http/routes/tokens.py) e [assinatura das claims](../api-hive-center/src/hive_center_api/adapters/security/token_signer.py). A inclusão no catálogo precisa contemplar a atualização dos perfis persistidos, não apenas a lista de constantes.

## 2. Uma ferramenta nova: hive_ingest_document

Criar uma ferramenta específica para enviar conteúdo. Manter `ingest_workspace` com sua semântica de varredura do workspace configurado.

Entrada mínima proposta:

```json
{
  "program_id": "teste-remoto",
  "classification": "internal",
  "document_type": "note",
  "source_format": "txt",
  "content": "farol-violeta-427: nota sintética para verificar ingestão e busca remotas."
}
```

Primeiro corte:

- aceitar `txt` e `md`, com texto UTF-8 e limite inicial de 16 KiB;
- exigir programa e classificação explícitos;
- aceitar `document_type: note` ou `evidence`;
- recusar campos desconhecidos, caminhos locais, conteúdo vazio e binário;
- derivar autor/origem do `sub` autenticado no servidor;
- não aceitar autor, permissões ou aprovação de escopo declarados pelo documento.

Publicar schema claro e uma descrição que informe: envia o conteúdo ao acervo e retorna confirmação de publicação. O agente só informa sucesso quando o relatório confirmar publicação ou mostrar que a mesma nota já está ativa.

A ponte pode usar as claims locais para decidir se apresenta a ferramenta ao agente. Essa decisão serve à interface; a API valida o JWT e exige as permissões em toda chamada. Não transformar todos os clientes remotos em writers.

## 3. Um endpoint de ingestão de conteúdo no adaptador existente

Adicionar `POST /api/v1/documents/ingest` ao adaptador HTTP Mind existente. Preservar `POST /api/v1/ingest` como varredura administrativa do diretório.

Fluxo síncrono:

1. Validar JWT assinado, issuer esperado, audiência `mind`, expiração e `sub` não vazio.
2. Exigir `product.mind` + `mind.ingest`; verificar também se a instância está configurada como writer.
3. Validar tamanho, campos, formato e metadados do pedido.
4. Converter o texto usando `ConvertDocument`, preenchendo metadados com autoridade do servidor.
5. Gravar o envelope completo em `HIVE_DATA_DIR/programs/<program_id>/imports/remote/<member_hash>/<request_hash>.json`.
6. Chamar `IngestPathReport` para esse único arquivo, reaproveitando embeddings, publicação por revisões e confirmação atuais.
7. Retornar o relatório estruturado, incluindo os erros por arquivo.

`member_hash` deriva do sujeito autenticado. `request_hash` deriva do conteúdo e de todos os metadados relevantes, com serialização determinística. O servidor constrói o caminho; nenhuma parte livre enviada pelo agente vira caminho absoluto. Validar o identificador de programa e usar operações de filesystem confinadas à raiz do writer, incluindo proteção contra symlinks.

Publicar o arquivo de forma atômica e privada. Mesmo membro, conteúdo e metadados iguais reutilizam o arquivo; não gerar uma nota nova a cada repetição ou incluir um timestamp variável no hash. Serializar ingestões concorrentes do mesmo documento e coordená-las com o watcher. Se o arquivo já existe, confirmar os bytes antes de reutilizá-lo.

Este corte cria notas imutáveis: conteúdo diferente resulta em outra nota. Edição, exclusão e deduplicação entre membros ficam para a próxima etapa. Não anunciar unicidade global.

Permissão de escrita do membro e papel writer do serviço são condições distintas. Estender também a proteção do endpoint legado de varredura; não deixar o leitor dispará-lo apenas porque o processo do servidor é writer. Nas rotas de leitura, verificar `product.mind`.

## 4. Resposta e erros

Reutilizar o relatório existente de ingestão, com `ok`, contadores e resultados por arquivo. Adicionar apenas os identificadores/referências necessários para consultar a origem depois.

- Publicação confirmada: resultado `created` e `published: true`.
- A mesma nota já estava ativa: resultado `unchanged`.
- Falha: `ok: false`, relatório preservado e motivo legível; no MCP, `isError: true`.
- Sem permissão: HTTP `403`.
- JWT ausente ou expirado: HTTP `401`, com orientação de novo `hive login`.
- Pedido inválido ou acima do limite: HTTP `400`/`413`.

Não perder o relatório de uma falha parcial no cliente HTTP. O `RemoteClient` atual transforma respostas HTTP diferentes de 200 em erro genérico; ajustar esse comportamento para preservar o relatório desta ferramenta.

O processamento é síncrono e possui prazo finito. Se o prazo ou a conexão se perder, informar que a confirmação não foi recebida; não afirmar que nenhuma escrita ocorreu. Repetir o mesmo pedido permite reconciliar a nota sem criar outra cópia. Não implementar `job_id` ou ferramenta de status de job neste corte.

## 5. Sequência para quem vai implementar

| Ordem | Trabalho | Onde |
| --- | --- | --- |
| 1 | Registrar `mind.ingest` no catálogo, atualizar os perfis persistidos selecionados e testar as claims emitidas. | `api-hive-center`. |
| 2 | Validar permissões nas rotas HTTP e criar o endpoint de conteúdo que reutiliza conversor e ingestão individual. | Adaptador existente: `server/http.go`, `server/auth.go` e pipeline do `hive_mind`. |
| 3 | Acrescentar envio HTTP, ferramenta MCP e schema, mantendo a varredura local separada. | `server/remote.go`, `server/backend.go`, `server/mcp.go`. |
| 4 | Atualizar instruções dos agentes com exemplo e tratamento de sucesso/falha. | Templates em `server/skills/`. |
| 5 | Publicar versões compatíveis de cliente/serviço, configurar writer e atribuir o perfil ao usuário de teste. | Fluxos de release/deploy existentes. |
| 6 | Fazer novo login, reconectar MCP, enviar nota sintética e buscá-la remotamente. | Cliente instalado e programa de teste isolado. |

Este é um incremento do adaptador que já existe, para o teste solicitado. A futura API comercial e o plano de controle continuam no projeto irmão; não introduzir neste checkout outro backend de identidade ou portal.

## 6. Critério de conclusão

A entrega só está concluída quando:

1. O JWT de um leitor contém acesso Mind e sua chamada direta de ingestão é negada.
2. O JWT de um contribuinte contém acesso Mind e `mind.ingest`.
3. O agente desse contribuinte vê `hive_ingest_document`, envia a nota sintética e recebe publicação confirmada.
4. A busca remota no mesmo programa recupera o marcador `farol-violeta-427` e sua origem.
5. Repetir o pedido não cria documento/chunks adicionais.
6. Token expirado, payload inválido e tentativa de escapar do diretório falham sem publicação.
7. Uma falha de embeddings preserva dados anteriormente publicados e devolve o motivo ao agente.

O programa sintético pode permanecer com escopo `unknown`. Nesse caso, a consulta de memória deve incluir o filtro de escopo correspondente; não aprovar um alvo real para viabilizar este teste. Indexar uma nota não concede autorização para testar seus ativos.

Testes locais usam fixtures e diretórios temporários. Verificar ingestão → embeddings reais → Qdrant real → busca em ambiente isolado antes de declarar funcionamento no deploy. Não executar o binário deste checkout sobre o acervo de produção.

A evolução durável com PostgreSQL, jobs e revisão editorial pode ser retomada depois a partir da [decisão 003](docs/decisions/003-canonical-persistence.md) e do [plano do piloto da API](../api-hive-center/plano-versao-piloto.md).
