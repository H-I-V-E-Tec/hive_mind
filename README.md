# Hive Mind

Memória privada e compartilhada de reconhecimento autorizado, escrita em Go. O Hive Mind indexa notas e evidências locais, gera embeddings com Ollama e consulta um Qdrant privado. Agentes acessam essas informações por MCP local via `stdio`, sem expor uma porta MCP na rede.

## O que o projeto tem

- Um ou mais writers, cada um dono dos documentos que publica, e readers somente para consulta, todos com credenciais individuais.
- Ingestão de Markdown, texto e JSON; observação de arquivos, `.gitignore`, limites de tamanho/chunks e proteção de paths.
- Publicação por revisões, manifesto de embeddings, registro de writer, tombstones e remoção verificada.
- Escopo por programa aprovado explicitamente a partir de `scope.json`; uma nota não concede autorização.
- Busca semântica, esparsa e híbrida com filtros de programa, classificação, escopo, tipo e tags.
- MCP: `hive_search`, `hive_get_context`, `get_sync_status` e `ingest_workspace`.
- CLI operacional, TLS fora de loopback, validação de permissões e auditoria sanitizada com métricas de uso (`audit report`).
- Templates de skill para Claude Code e Codex que ensinam o agente a consultar escopo antes de agir e a escrever notas reutilizáveis.
- Backup pareado com restic, CI de segurança, container não root e bloqueio de release sem evidências operacionais.

```text
Writer A: arquivos + agente → Hive Mind + Ollama local ─┐
Writer B: arquivos + agente → Hive Mind + Ollama local ─┼─ TLS / rede privada → Qdrant
Reader:   agente → MCP local + Ollama local ────────────┘                      dados + controle
```

Cada writer publica somente os documentos que ele mesmo criou; documentos de outro writer encontrados na pasta são ignorados, nunca sobrescritos. Para o roteiro de um ensaio com quatro pessoas e dois programas, veja [o ensaio de quatro pessoas](docs/operations/four-person-trial.md).

Há implementação e testes automatizados, mas o aceite completo da v0.1 ainda está pendente. Ensaios entre duas máquinas, restauração real, revogação e controles da implantação não são substituídos por mocks. Veja [estado e pendências](docs/operations/implementation-status.md).

## Passo a passo para usar

Para um roteiro completo e copiável de laboratório, incluindo tokens writer/reader e integração com Codex, consulte [Configurar Qdrant e Codex](docs/guias/CONFIGURAR_QDRANT_E_CODEX.md).

### 1. Preparar requisitos e compilar

É necessário Go 1.25 ou superior, compilador C/C++ (CGO/tree-sitter), Git, Ollama local e Qdrant privado. Para containers, use Docker com Compose. Backup requer Python 3.10+ e restic instalado separadamente.

Na raiz deste repositório:

```bash
go mod download
go build -trimpath -o bin/hive-mind .
./bin/hive-mind help
```

Não compile com `CGO_ENABLED=0`. Os comandos abaixo pressupõem execução na raiz do projeto; no cliente MCP, use o caminho absoluto do binário.

### 2. Preparar Qdrant e Ollama

Use Qdrant privado, com tokens individuais limitados às collections `hive_mind_v01` e `hive_mind_v01__control`. O processo Hive nunca recebe a chave administrativa. Siga o [guia de provisionamento e acesso](docs/operations/qdrant-access.md).

Para um laboratório local, injete `HIVE_QDRANT_ADMIN_KEY` no ambiente do operador por um gerenciador de segredos e execute:

```bash
docker compose up -d qdrant ollama
docker compose exec ollama ollama pull nomic-embed-text
docker compose exec ollama ollama list
```

O Compose publica Qdrant REST `6333`, gRPC `6334` e Ollama `11434` somente em `127.0.0.1`. Não execute `docker compose config` sem `--quiet` em logs compartilhados: a saída pode conter segredos interpolados. O download do modelo é explícito, não ocorre ao iniciar o Hive.

Se Ollama já estiver instalado e em execução no host:

```bash
ollama pull nomic-embed-text
ollama list
```

Todos os dispositivos precisam do mesmo modelo e digest, não apenas do mesmo nome. Não atualize o modelo de uma collection existente sem migração. Para outra máquina acessar Qdrant, configure TLS válido e firewall/VPN; HTTP remoto é rejeitado.

### 3. Configurar o writer

Crie uma pasta de documentos e outra, separada, para auditoria:

```bash
mkdir -p hive-data/programs/acme-bugbounty/notes
mkdir -m 700 hive-audit
cp .env.hive.example .env.hive
chmod 600 .env.hive
```

Edite `.env.hive` com seus identificadores e caminhos absolutos. Ele não é carregado automaticamente.

| Variável | Finalidade |
| --- | --- |
| `HIVE_ID` | Identidade comum do Hive, por exemplo `research-team`. |
| `HIVE_DEVICE_ID` | Identidade única deste dispositivo. |
| `HIVE_ROLE` | `writer` ou `reader`. Vários writers são permitidos; cada um em um dispositivo próprio. |
| `HIVE_WRITER_APPROVAL_ID` | Identificador do change que autorizou **este** writer; um por dispositivo, imutável após o registro. |
| `HIVE_COLLECTION` | Collection de dados; controle recebe o sufixo `__control`. |
| `HIVE_DATA_DIR` | Pasta canônica absoluta; necessária no writer. |
| `HIVE_AUDIT_DIR` | Pasta privada, fora de `HIVE_DATA_DIR`. |
| `QDRANT_URL` | Endpoint gRPC: local `http://127.0.0.1:6334` ou remoto `https://...`. |
| `QDRANT_API_KEY` | Token individual, injetado por ambiente/secret manager. |
| `QDRANT_TLS_CA_FILE` | CA privada, quando necessária; nunca desative a verificação TLS. |
| `OLLAMA_URL` | `http://127.0.0.1:11434`. |
| `EMBEDDING_MODEL` | Modelo previamente instalado, por exemplo `nomic-embed-text`. |
| `HIVE_MAX_CLASSIFICATION` | `internal` por padrão; `restricted` exige dispositivo autorizado. |
| `HIVE_CONTEXT_MAX_CHARS` | Limite do contexto serializado; padrão `12000`. |

Para loopback, ajuste `QDRANT_URL` e remova `QDRANT_TLS_CA_FILE` do exemplo. Remova também o placeholder `QDRANT_API_KEY` se o token vier do secret manager.

Carregue somente um arquivo revisado por você: `source` executa sintaxe de shell. Depois injete o token individual:

```bash
set -a
source ./.env.hive
set +a
```

Alternativa: TOML plano explicitamente selecionado com `./bin/hive-mind --config /caminho/hive.local.toml status`. Precedência: flags > ambiente > TOML. Chaves legadas e autodescoberta não são aceitas. Consulte `help` e a [spec de configuração](docs/spec/01-hive-configuration.md).

### 4. Adicionar documentos e aprovar escopo

Estrutura sugerida:

```text
hive-data/
  programs/
    acme-bugbounty/
      scope.json
      rules.md
      notes/api.md
      evidence/endpoint.md
```

Exemplo de `notes/api.md` — use apenas dados de ativos realmente autorizados:

```markdown
---
program_id: acme-bugbounty
document_type: note
classification: internal
source: manual
collected_at: 2026-09-11T12:00:00Z
tags: [recon, oauth]
asset_refs: [api.example.com]
---
# API
Observação sobre o fluxo OAuth do ambiente autorizado.
```

Crie `scope.json` conforme o [contrato e exemplo](docs/spec/contracts/scope-manifest.md), refletindo a política real do programa. `claimed_scope_status` em notas nunca autoriza um ativo.

Depois do provisionamento administrativo das collections:

```bash
./bin/hive-mind ingest
./bin/hive-mind scope approve acme-bugbounty
```

O primeiro `ingest` também registra este dispositivo como writer (`writer_registration`); `validate` só passa em um writer depois disso. O segundo comando mostra resumo/hash, sem aprovar, e retorna `2` enquanto não houver confirmação. Revise a política e confirme:

```bash
./bin/hive-mind scope approve acme-bugbounty --yes
./bin/hive-mind validate
./bin/hive-mind status
```

`--yes` aprova o conteúdo lido nessa execução; mantenha o arquivo estável entre revisão e confirmação. Mudança detectada durante a operação interrompe a publicação. Quem aprova é o dono da aprovação: alterar/remover o manifesto **nesse** dispositivo faz o programa voltar a `unknown` até nova aprovação. Outros writers não precisam do `scope.json` local — usam a revisão aprovada — e não conseguem invalidá-la por acidente.

`validate` não cria collections nem manifesto. Em uma instalação vazia, provisione as collections e execute a primeira ingestão antes de validar.

### 5. Pesquisar e conectar um cliente MCP

Teste pelo terminal:

```bash
./bin/hive-mind search acme-bugbounty "fluxo OAuth" --tag=oauth --scope-status=authorized --limit=8
```

Sem subcomando, o executável inicia o MCP por `stdio`. Configure o cliente para executar o binário absoluto, herdando o ambiente configurado. Exemplo genérico, adaptável ao formato do cliente:

```json
{
  "mcpServers": {
    "hive-mind": {
      "command": "/caminho/absoluto/hive_mind/bin/hive-mind",
      "args": []
    }
  }
}
```

Não coloque tokens no JSON versionado. Aplicativos gráficos podem não herdar o ambiente do terminal: configure a injeção pelo mecanismo seguro do cliente. No writer, o processo observa a pasta; no reader, não inicia ingestão.

Entrada para `hive_search`:

```json
{"program_id":"acme-bugbounty","query":"fluxo OAuth","tags":["oauth"],"effective_scope_status":"authorized","limit":8}
```

Entrada para `hive_get_context`:

```json
{"program_id":"acme-bugbounty","question":"O que sabemos deste host?","asset":{"type":"host","value":"api.example.com"},"limit":8}
```

Resultados incluem proveniência, revisão e `untrusted_content: true`: conteúdo recuperado é evidência não confiável, nunca instrução a ser executada. Hive, collection e elevação de classificação não podem ser escolhidos pelo agente.

### 6. Adicionar outro writer ou um reader

**Outro writer** (cada pessoa que vai escrever): na outra máquina, instale o mesmo binário/modelo, configure o mesmo Hive/collection, outro `HIVE_DEVICE_ID`, outro `HIVE_WRITER_APPROVAL_ID`, um token próprio `rw` nas duas collections e uma pasta `HIVE_DATA_DIR` própria (pode ser um clone do mesmo repositório privado). Execute `ingest` para registrar o writer e depois `validate`. Documentos com o mesmo path já publicados por outro writer aparecem como `skipped` e não são alterados; use `<handle>-` no nome dos arquivos para evitar colisões. `remove` e `ingest --prune` só agem sobre os documentos deste writer.

**Reader**: mesma instalação, `HIVE_ROLE=reader` e um token próprio `r` nas duas collections. Remova `HIVE_WRITER_APPROVAL_ID` e `HIVE_DATA_DIR`; mantenha auditoria local e acesso TLS/VPN. Execute `validate`, `status` e a mesma busca. A validação exige leitura permitida e negação explícita da prova de escrita nas duas collections.

Nunca reutilize a credencial de outro dispositivo, mesmo entre writers.

### 7. Instalar a skill do agente

```bash
./bin/hive-mind install-skill claude /caminho/do/projeto   # gera .claude/skills/hive-mind/SKILL.md
./bin/hive-mind install-skill codex /caminho/do/projeto    # gera .codex/mcp-instructions.md; referencie-o no AGENTS.md
```

Os templates ensinam o agente a chamar `hive_get_context` antes de tocar em um ativo, a tratar resultados como não confiáveis e a escrever notas com o front-matter aceito, incluindo a convenção de `tags` com time e handle. Os demais templates (`cursor`, `windsurf`, `cline`, `copilot`, `generic`) são legado do servidor RAG original.

## Referência de comandos

Use `./bin/hive-mind` antes de cada comando:

| Comando | Efeito |
| --- | --- |
| `help` | Exibe comandos e flags, sem precisar de serviços. |
| Sem comando | Inicia o MCP local. |
| `version` | Exibe versão da release e revisão de origem em JSON; não exige serviços. |
| `validate` | Verifica configuração, auditoria, serviços, schema/fingerprint e permissões. |
| `status` | Estado sanitizado em JSON; requer infraestrutura válida. |
| `ingest` | Reconcilia os documentos deste writer; informa quantos foram ignorados por pertencerem a outro writer. |
| `ingest --prune` | Remove documentos deste writer ausentes após carência. |
| `remove programs/acme-bugbounty/notes/api.md` | Publica tombstone e verifica exclusão dos vetores; só para documentos deste writer; não apaga o arquivo local. |
| `scope approve acme-bugbounty` | Mostra resumo/hash para revisão, sem confirmar. |
| `scope approve acme-bugbounty --yes` | Confirma o manifesto atual e rematerializa escopo. |
| `search acme-bugbounty "consulta"` | Busca filtrada por programa. |
| `audit record credential_rotation change-1043` | Registra rotação feita pelo operador; não altera o token. |
| `audit record credential_revocation change-1044` | Registra revogação feita pelo operador. |
| `audit record writer_promotion change-1045` | Registra promoção feita pelo operador. |
| `audit report --since=2026-09-14 --program=acme-bugbounty` | Agrega consultas por dia/dispositivo/programa/ferramenta: quantidade, caracteres entregues, bytes de origem e razão de economia. Só números. |
| `list-skills` | Lista os templates de integração incluídos. |
| `install-skill <agent> [destino]` | Instala template (`claude`, `codex`; demais são legado); aceita `all`. Requer configuração válida. |

Filtros CLI: `--document-type=note`, `--tag=oauth`, `--classification=internal`, `--scope-status=authorized`, `--limit=8`. Tipo e tag podem ser repetidos; tags usam semântica ALL. Limite: 1–20. Use `--chave=valor` para os filtros.

Códigos: `0` sucesso; `2` uso/entrada; `10` configuração; `11` conectividade; `12` autenticação/permissão; `13` TLS; `14` schema/fingerprint; `15` falha parcial recuperável. Não trate `15` como sucesso: examine auditoria/estado antes de repetir mutações.

## Container MCP opcional

O perfil `mcp` do Compose é uma referência Linux, pois usa rede do host. Configure `HIVE_DATA_DIR` no host e injete `HIVE_QDRANT_WRITER_TOKEN` limitado às duas collections. Ele usa `local-hive` / `docker-writer`: não o inicie concorrendo com outro writer.

```bash
docker compose --profile mcp build qdrant-mcp-server
docker compose --profile mcp run --rm -T qdrant-mcp-server ingest
docker compose --profile mcp run --rm -T qdrant-mcp-server validate
docker compose --profile mcp run --rm -T qdrant-mcp-server
```

Runtime: usuário `hive` (UID 10001), raiz somente leitura, capabilities removidas, limites de recursos, documentos somente leitura e volume privado de auditoria. Garanta que UID 10001 consiga ler os documentos. Em macOS/Windows, prefira inicialmente o binário local com os serviços publicados em loopback.

## Auditoria e backup

Logs JSONL usam diretório `0700`, arquivos `0600`, rotação em 10 MiB e expiração em 30 dias verificada a cada gravação. Sem `HIVE_AUDIT_DIR`, o binário usa o cache local do usuário; configure caminho explícito em produção.

Não são registrados textos de documentos/consultas, tokens, embeddings nem mensagens brutas de provedores. Cada consulta registra apenas contadores: quantidade, duração, caracteres da resposta serializada, bytes dos documentos de origem e truncamento; `audit report` agrega esses números para medir a economia de contexto (veja a [spec 09](docs/spec/09-usage-metrics.md)). Identificadores e paths relativos ainda são sensíveis: proteja disco e acesso. Falha de auditoria bloqueia operações críticas; registros `prepared` sem conclusão exigem reconciliação. Para processos parados e logs do backup, configure expiração operacional de 30 dias.

O backup preserva o par de collections em restic criptografado. Requer parada real do writer, staging em disco criptografado e credenciais separadas. Após seguir o [runbook de backup e recuperação](docs/operations/backup-recovery.md):

```bash
python3 scripts/hive_backup.py backup
python3 scripts/hive_backup.py restore --snapshot <id-retornado-pelo-backup>
```

`restore` somente recupera/verifica arquivos em staging; não restaura automaticamente Qdrant de produção. Defina responsável, RPO/RTO e retenções no [perfil operacional](docs/operations/deployment-profile.md).

## Testes e liberação

```bash
go mod verify
go test -race ./...
go vet ./...
go build ./...
python3 -m unittest discover -s scripts -p 'test_*.py'
python3 -m unittest discover -s deploy -p 'test_*.py'
bash deploy/test_deploy_server.sh
```

A CI também executa govulncheck, Gitleaks no histórico/arquivos e verificação do container. A release exige [evidências operacionais](docs/operations/security-release.md) atuais e vinculadas ao código:

```bash
go run ./cmd/security-gate --digest
go run ./cmd/security-gate --version v0.1.0
```

O segundo comando deve falhar enquanto `docs/operations/release-evidence.json` não existir ou houver gates pendentes. Não substitua os ensaios reais por resultados inventados. Releases assinadas, atualização do `hive_instance` e promoção do Qdrant estão descritas em [entrega e deploy](docs/operations/delivery-and-deployment.md).

Documentação: [índice](docs/README.md), [specs](docs/spec/README.md), [segurança](docs/spec/security/README.md), [decisões](docs/decisions/README.md). Use somente em programas e ativos explicitamente autorizados.
