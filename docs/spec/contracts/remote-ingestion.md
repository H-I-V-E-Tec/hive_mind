# Ingestão de conteúdo pelo MCP remoto

Estado: implementado e validado localmente com serviços reais isolados; deploy pendente. Data: 2026-10-06.

`hive_ingest_document` envia conteúdo pelo cliente instalado ao endpoint
`POST /api/v1/documents/ingest`. Não lê caminhos locais nem dispara uma varredura.

Entrada obrigatória: `program_id`, `classification` (`internal` ou `restricted`),
`document_type` (`note`, `evidence` ou `scope`), `source_format` (`txt`, `md` ou `json`) e `content`
(UTF-8; até 16 KiB para nota/evidência e 1 MiB para escopo). `platform` e `target_name` são opcionais, mas devem ser
informados juntos para registrar a nota no catálogo de alvos. Campos desconhecidos,
conteúdo vazio/binário e metadados inválidos são recusados antes da gravação.

Notas e evidências exigem `mind.ingest`. Um manifesto usa `document_type=scope`,
`source_format=json`, exige `mind.scope.approve` e é publicado no path canônico
`programs/<program_id>/scope.json`. A publicação o mantém não aprovado. A aprovação
é separada em `POST /api/v1/scopes/<program_id>/approve`, com o SHA-256 dos bytes
exatos, e exige a mesma permissão. Alteração concorrente falha por conflito.

O servidor exige JWT RS256 do Center configurado, audiência `mind`, expiração,
sujeito não vazio e `product.mind`, além da permissão da operação. A instância também
precisa ser writer. As rotas de consulta exigem `product.mind`; a varredura legada
também exige `mind.ingest`. As claims locais servem apenas à apresentação da
ferramenta; a autorização acontece no servidor em toda chamada.

O conteúdo passa pelo conversor existente. A origem deriva do sujeito autenticado.
O servidor constrói o caminho `programs/<program_id>/imports/remote/<member_hash>/<request_hash>.json`,
com hashes determinísticos do membro e de todos os campos do pedido. Publicação
atômica de envelope completo, arquivo privado, sem sobrescrever documentos. Uma
repetição confirma os bytes existentes e usa a mesma revisão; conteúdo diferente
cria outra nota. Ingestões concorrentes da mesma origem, inclusive pelo watcher,
são serializadas antes de consultar/publicar a revisão.

O resultado é o relatório v1 de ingestão: sucesso apenas com publicação confirmada
ou `unchanged` de uma revisão ativa. Falhas preservam o relatório HTTP/MCP e usam
`isError: true`. `401` pede novo login; `403` indica falta de permissão/writer;
`400`/`413` recusam entrada; `500` preserva falhas de indexação. O prazo síncrono
é 45 segundos. Perder a resposta não prova ausência de escrita: repetir exatamente
o pedido permite reconciliar. Não há jobs, edição, exclusão ou deduplicação global.

Teste sintético: ingerir `farol-violeta-427` em `teste-remoto`, consultar com
`effective_scope_status: unknown` se não houver escopo aprovado e repetir sem
novos chunks/embeddings. Cobrir leitor, token expirado/issuer/audiência inválidos,
payload inválido, limites, symlink, concorrência e falha de embedding. Validar
Qdrant e embeddings reais isolados antes de declarar o fluxo integrado. Ingestão
não concede autorização para atuar nos ativos mencionados.

Evolução posterior: [plano arquitetural](../../../PLANO_MELHORIA_ARQUITETURAL.md).

## Validação com serviços reais

Use containers descartáveis exclusivos, com portas de loopback que não sejam as
dos serviços existentes. O teste aceita somente os endereços abaixo e cria
collections com prefixo `hive_remote_test_`, removidas ao terminar. Não usa `.env`
operacional, dados de produção ou tokens do usuário.

```sh
docker run -d --name hive-ingest-test-qdrant \
  -p 127.0.0.1:26333:6333 -p 127.0.0.1:26334:6334 \
  -e QDRANT__SERVICE__API_KEY=remote-ingest-test-only qdrant/qdrant:v1.18.3
docker run -d --name hive-ingest-test-ollama \
  -p 127.0.0.1:21434:11434 ollama/ollama:0.34.0
docker exec hive-ingest-test-ollama ollama pull all-minilm
HIVE_TEST_REAL_INGEST=1 go test ./server -run '^TestRemoteIngestionRealServices$' -count=1 -v
docker rm -f hive-ingest-test-qdrant hive-ingest-test-ollama
```

Resultado em 2026-10-06: MCP → HTTP/JWT → Ollama real → Qdrant real → busca
remota passou. Replay sem novas chamadas de embedding ou chunks; leitor negado
na chamada HTTP direta. A comunicação sem TLS desse teste é exclusiva do loopback
com credencial sintética; o produto remoto continua exigindo HTTPS.

Também passaram `go test -race ./...` e `go vet ./...`; os testes direcionados
finais de ingestão/JWT/MCP passaram com detector de concorrência. Cobertura inclui
limite em bytes, JSON inválido, UTF-8, claims adulteradas, issuer/audiência/expiração,
symlinks, colisão, replay concorrente, falha de embedding e prazo na espera do watcher.

## Publicação e primeiro uso

1. Publicar a API Center, executar `alembic upgrade head` e atribuir ao membro um
   perfil com `product.mind` e `mind.ingest`.
2. Publicar esta versão do serviço Mind e do binário cliente. O serviço precisa
   de `HIVE_ROLE=writer`, writer/approval válidos, `HIVE_CENTER_URL` igual ao issuer
   dos JWTs, acesso aos serviços internos e diretório de dados gravável. Reutilizar
   a configuração aprovada do writer; não criar outro writer para o mesmo acervo.
3. Atualizar o cliente instalado, executar `hive login` e reconectar o MCP para
   carregar a ferramenta e o novo token. Tokens antigos não ganham a permissão.
4. Enviar a nota sintética e conferir publicação, busca e replay. Este teste no
   ambiente implantado permanece pendente; a validação local não é aceite do deploy.
