# 📋 Hive Mind — Roadmap & TODO

Itens abertos e relatos de uso real do Hive Mind. O andamento estruturado está no [plano de melhoria](plano%20de%20melhoria.md) e em [docs/operations/implementation-status.md](docs/operations/implementation-status.md).

---

## 🗄️ Histórico: servidor RAG genérico (pré-Hive)

> As seções abaixo até a linha de separação descrevem o servidor RAG original e **não** refletem o código atual. Diferenças conhecidas: a variável é `HIVE_MAX_EMBEDDING_WORKERS` (não `MAX_EMBEDDING_WORKERS`); o hash de conteúdo é SHA-256, não SHA1; as flags `--batch-size`, `--batch-timeout` e `--log-to-file` não existem — as flags aceitas estão em `hive-mind help`. A busca híbrida está implementada em `queryVariant`, mas a configuração é fixa em `dense` (ver item aberto mais abaixo).


## ⚡ Performance & Ingestion Concurrency

- [x] **Rate-Limited Embedding Worker Pool**
  - Implement a buffered worker pool (e.g., maximum concurrent workers controlled by a `MAX_EMBEDDING_WORKERS` environment variable).
  - Prevent local Ollama engine thrashing and timeout errors during bulk changes (like git branch switches or initial indexing).
- [x] **Initial Startup Synchronization Crawler**
  - Read files on startup and verify their status.
  - Compare content hashes (SHA-256) with existing points in Qdrant using Scroll query.
  - Automatically index newly created or modified files, and skip indexing unchanged files completely before opening the filesystem notification loop.

---

## 🧠 Semantic & AST-Aware Chunking

- [x] **Block & Structure-Aware Parser**
  - Replace the basic 1000-character line accumulator with a parser that respects semantic boundaries.
  - Support syntax parsing blocks (e.g., class/function boundaries for Go, C#, JS/TS, Python) so that logical segments are not cut in half.
  - Handle Markdown structure (headers, list bounds) when indexing directories like `.codex` or `.obsidian`.

---

## 🔍 Advanced Querying & Filtering

- [x] **Metadata-Driven Filter Arguments**
  - Expose the `hive_search` tool with optional filters for document types, tags, classification, and effective scope.
  - Enforce Hive, program, approved scope revision, and classification boundaries in native Qdrant filters.
  - Use Qdrant's high-speed payload keyword matching filters.
- [~] **Hybrid Search (Dense + Sparse)** — implementado, não habilitado
  - `queryVariant` combina denso + esparso por RRF; vetores esparsos são gravados na ingestão.
  - `SearchMode` é fixo em `dense` em `server/config.go` e não há env/flag; o esparso é ponderação de termos por hash, não BM25. Expor e avaliar é item da Fase 1 do plano.

---

## 🏷️ Rich Metadata & Precision Navigation

- [x] **Positional Payload Fields**
  - Extract and save precise positional indexes for code chunks in the payload:
    * `start_line` / `end_line`: Allow the LLM client to direct the user to the exact lines matching the chunk.
    * `language`: Store explicit languages for quick client UI rendering filters.
    * `content_hash`: Compare SHA1 checksums of file contents to skip re-indexing if no change has actually occurred.
- [x] **Last Synced Date/Time presentation**
  - Present the last updated/synchronized timestamp of matched points to the agent during semantic searches to verify chronologic relevance.

---

## 🔒 Production, Security & Resiliency

- [x] **Authenticated Qdrant Cloud Integration**
  - Support TLS/HTTPS connections.
  - Handle `QDRANT_API_KEY` configurations to connect safely to hosted Qdrant Cloud nodes.
- [x] **Exponential Backoff Connection Retries**
  - Wrap connection handshakes in a retry engine with dynamic fallback to automatically reconnect if Qdrant or Ollama drops out or restarts.

---

## 📈 Developer Experience & MCP Capabilities

- [x] **Dynamic Progress Reporting**
  - Expose a simple status query tool (e.g. `get_sync_status`) so users can check if the workspace is fully indexed.
- [x] **Server Command Line Interface**
  - Support execution flags (e.g. `--batch-size`, `--batch-timeout`, `--log-to-file`) when launching the binary directly from terminal setups.

---

*Fim do histórico. Os itens a seguir referem-se ao Hive Mind atual.*

---

## 🧭 Catálogo de alvos v1.0.0

Contrato e implementação inicial: [catálogo de alvos](docs/vision/target-catalog-v1.md).

- [x] Extrair ativos normalizados de listas de recon reconhecidas, com origem e
  sem transformar texto livre em autorização.
- [ ] Reingerir o corpus piloto para preencher observações de alvo antigas.
- [x] Implementar `hive_list_targets` com escopo aprovado, documentos ativos
  distintos, limites de leitura e ranking balanceado explicável.
- [x] Testar isolamento básico, exclusões, classificação, revisões antigas,
  remoção/tombstones e leitura sem manifesto local; atualizar as instruções dos agentes.
- [ ] Ensaiar limites de catálogo e corpus real.

---

## 🧪 Validação pendente do laboratório local (2026-09-12)

Ambiente de lab montado sem Docker: Ollama + Qdrant como binários standalone em
`/Users/tiagobalbinoferreira/Documents/HIVE/hive-lab` (script `start-services.sh`),
MCP `hive-mind` registrado no Claude Code (escopo local, ✔ Connected).
Estado da ingestão: coinspot, demo, etoro (28 docs) e yahoo ingeridos; 5674 pontos.

- [ ] **Ingerir o restante do hive-data**
  - Falta apenas `programs/coinspot/recon/recon__urls.txt` (~8.4 MB).
  - Excede `HIVE_MAX_FILE_BYTES` padrão (5 MiB); no lab foi elevado para 20 MiB (env do MCP).
  - Rodar `hive-mind ingest` novamente com os serviços no ar para completar.
  - Nota 2026-09-16: `.txt` sem front matter recebe `classification: unknown` e nunca é devolvido pela busca; ingerir esse arquivo custa embeddings sem torná-lo pesquisável. Ver [linha de base de recuperação](docs/operations/retrieval-baseline.md).
- [ ] **Revisar classificação/sanitização de erros operacionais** (`server/operations.go`)
  - Erro de arquivo grande ("document exceeds N bytes") cai no bucket default e é
    mostrado como "required service is unavailable" — mensagem enganosa.
  - Falha de embedding do Ollama (HTTP 500) contém "dimension" e é sanitizada como
    "embedding or collection schema is incompatible" — também enganoso.
  - Considerar buckets/mensagens dedicados (arquivo grande = uso/config; embedding 5xx = conectividade).
  - Atualização 2026-09-15: relatório por arquivo usa motivos explícitos `file_size_limit`,
    `chunk_limit` e `embedding_failed`. Revisão do classificador geral continua pendente.
- [ ] **Validar `validate`/`status` fim-a-fim** após completar a ingestão (hoje: `ok: true`).
- [ ] **Persistência/inicialização dos serviços**
  - Hoje o start é manual (`hive-lab/start-services.sh`). Avaliar `launchd`/`brew services`
    para subir Qdrant/Ollama no boot, se desejado.
- [ ] **Confirmar tools MCP em sessão nova do Claude Code**
  - `mcp__hive-mind__hive_search`, `hive_get_context`, `get_sync_status`, `ingest_workspace`.

---

## 🐞 Bugs encontrados em teste de uso real (2026-09-12, benchmark etoro)

Contexto: benchmark de retrieval (Hive vs grep) + dedup do corpus etoro. Ordem por severidade.

- [ ] **Hybrid Search (BM25+denso) marcado `[x]` mas NÃO ativo no `hive_search`** 🔴
  - Sintoma: em corpus pequeno o Hive **perdeu pro grep** (hit@3 7/8 vs 8/8, MRR 0,71 vs 0,875)
    tanto em consulta lexical quanto semântica; e **errou uma paráfrase** ("buscar segredos da
    máquina na nuvem" não achou os docs de SSRF→IMDS). Comportamento é de **vetor puro**.
  - Ação: confirmar se o sparse/BM25 está realmente fundido no ranking do `hive_search`
    (não só ingerido); se estiver, revisar peso denso×esparso e reranking.
  - Inspeção 2026-09-15: `queryVariant` implementa RRF, mas a configuração usa `dense`.
    O sparse atual é uma ponderação de termos por hash, não BM25 completo.
  - Medição 2026-09-16 (harness offline, embedder sintético): `sparse` e `hybrid` superaram
    `dense` em MRR (0,909 vs 0,795) no corpus de fixtures; não mede o modelo real. Expor
    `SearchMode` e repetir com Ollama/Qdrant reais é item da Fase 1.
- [x] **Preservar o relatório do lote quando arquivos falham**
  - Inspeção 2026-09-15: os workers já continuavam após falha individual; o retorno de
    apenas `firstErr` ocultava o resumo na CLI/MCP, dando a impressão de interrupção.
  - Implementado relatório JSON por arquivo com todas as falhas, limites explícitos,
    criações, atualizações e inalterados. CLI sai com `15`; MCP usa `isError: true`
    preservando o relatório. Testes: `server/ingestion_report_test.go`.
- [ ] **Watcher não purga vetores ao remover/mover arquivo (macOS)** 🟠
  - Mover 9 docs pra fora do `hive-data` **não** disparou o purge; os vetores órfãos
    continuaram aparecendo na busca. Só saíram via `hive-mind remove <path>` manual.
  - Ação: verificar fsnotify de `Remove`/`Rename` no macOS; ou reconciliar órfãos no `ingest`.
  - Inspeção 2026-09-16: `server/watcher.go` trata Write/Create/Remove, não Rename. O `ingest`
    já marca ausentes como `missing` (pending_delete) e `--prune` remove após a carência;
    o watcher continua sem Rename (Fase 1).
- [ ] **Sem cap por-documento no top-k** 🟠
  - 1 doc longo domina os N slots (ex.: 7/8 = `arapuca.md`); só ~2 docs únicos por busca
    de 8. Derruba diversidade/precisão e "gasta" o limite com chunks do mesmo doc.
  - Ação: retornar no máx. K chunks por documento, ou 1 melhor-chunk/doc + expandir sob demanda.
  - Medição 2026-09-16: reproduzido no harness offline — `long-report.md` ocupou 8/8
    resultados em duas consultas; média de 3,7–4,6 documentos distintos por top-8.
- [ ] **Economia de token é função do `limit`, não do conteúdo** 🟡
  - Dedup do corpus (remover espelhos 2×) **não** baixou tokens (+3%): o servidor sempre
    devolve `limit` chunks. Melhora foi só de precisão/higiene.
  - Ação: expor budget de tokens / default de `limit` menor / cap por-doc (acima) pra economia real.
- [ ] **Frontmatter YAML repetido em cada chunk retornado** 🟡
  - Todo resultado repete o cabeçalho (~150 chars: program_id, source, collected_at, tags…)
    = imposto de token por chunk. Ação: enxugar metadados na resposta (só o essencial).
  - Medição 2026-09-16: no corpus de fixtures, 34% dos bytes indexados são prefixo
    (front matter + título) repetido por seção; também infla o embedding de cada chunk.
- [ ] **Embedding `nomic-embed-text` com lacuna semântica de domínio** 🟡
  - Falhou em conectar paráfrase de segurança (nuvem/segredos → IMDS/SSRF). Ação: avaliar
    modelo de embedding melhor/maior ou reranker; medir em corpus grande antes de trocar.
- [ ] **Higiene: segredo cru no corpus ingerido** 🟠 (achado colateral)
  - `programs/etoro/recon/context.txt` tinha 3 chaves de API em claro (mixpanel token/apiKey,
    googlePlacesApiKey) + e-mail da conta. Removido do índice no dedup. Ação: guarda de
    sanitização na ingestão (detectar/mascarar segredos antes de embeddar).
