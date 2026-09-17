# 📋 Go Qdrant-RAG MCP Server Roadmap & TODO

This document outlines the planned improvements, architectural enhancements, and functional updates to make this real-time RAG server highly robust, scalable, and intelligent.

---

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
- [x] **Hybrid Search (Dense + Sparse)**
  - Combine semantic dense vector search (Ollama embeddings) with sparse vector representations (like BM25).
  - Yield perfect search results for both high-level concepts and exact keyword matches (like specific variables, function names, or exact error codes).

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

## 🧪 Validação pendente do laboratório local (2026-09-12)

Ambiente de lab montado sem Docker: Ollama + Qdrant como binários standalone em
`/Users/tiagobalbinoferreira/Documents/HIVE/hive-lab` (script `start-services.sh`),
MCP `hive-mind` registrado no Claude Code (escopo local, ✔ Connected).
Estado da ingestão: coinspot, demo, etoro (28 docs) e yahoo ingeridos; 5674 pontos.

- [ ] **Ingerir o restante do hive-data**
  - Falta apenas `programs/coinspot/recon/recon__urls.txt` (~8.4 MB).
  - Excede `HIVE_MAX_FILE_BYTES` padrão (5 MiB); no lab foi elevado para 20 MiB (env do MCP).
  - Rodar `hive-mind ingest` novamente com os serviços no ar para completar.
- [ ] **Revisar classificação/sanitização de erros operacionais** (`server/operations.go`)
  - Erro de arquivo grande ("document exceeds N bytes") cai no bucket default e é
    mostrado como "required service is unavailable" — mensagem enganosa.
  - Falha de embedding do Ollama (HTTP 500) contém "dimension" e é sanitizada como
    "embedding or collection schema is incompatible" — também enganoso.
  - Considerar buckets/mensagens dedicados (arquivo grande = uso/config; embedding 5xx = conectividade).
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
- [ ] **`ingest_workspace` aborta no 1º arquivo grande em vez de pular** 🔴
  - `ingest recon__urls.txt: document exceeds 1000 chunks` derruba o batch inteiro; como o
    scan é **global** (todos os programas), 1 dump de recon > `HIVE_MAX_CHUNKS_PER_FILE`
    impede a ingestão de qualquer programa. Nunca passou limpo com o corpus atual.
  - Ação: pular (warn) arquivos acima do limite e seguir; reportar lista no fim do summary.
- [ ] **Watcher não purga vetores ao remover/mover arquivo (macOS)** 🟠
  - Mover 9 docs pra fora do `hive-data` **não** disparou o purge; os vetores órfãos
    continuaram aparecendo na busca. Só saíram via `hive-mind remove <path>` manual.
  - Ação: verificar fsnotify de `Remove`/`Rename` no macOS; ou reconciliar órfãos no `ingest`.
- [ ] **Sem cap por-documento no top-k** 🟠
  - 1 doc longo domina os N slots (ex.: 7/8 = `arapuca.md`); só ~2 docs únicos por busca
    de 8. Derruba diversidade/precisão e "gasta" o limite com chunks do mesmo doc.
  - Ação: retornar no máx. K chunks por documento, ou 1 melhor-chunk/doc + expandir sob demanda.
- [ ] **Economia de token é função do `limit`, não do conteúdo** 🟡
  - Dedup do corpus (remover espelhos 2×) **não** baixou tokens (+3%): o servidor sempre
    devolve `limit` chunks. Melhora foi só de precisão/higiene.
  - Ação: expor budget de tokens / default de `limit` menor / cap por-doc (acima) pra economia real.
- [ ] **Frontmatter YAML repetido em cada chunk retornado** 🟡
  - Todo resultado repete o cabeçalho (~150 chars: program_id, source, collected_at, tags…)
    = imposto de token por chunk. Ação: enxugar metadados na resposta (só o essencial).
- [ ] **Embedding `nomic-embed-text` com lacuna semântica de domínio** 🟡
  - Falhou em conectar paráfrase de segurança (nuvem/segredos → IMDS/SSRF). Ação: avaliar
    modelo de embedding melhor/maior ou reranker; medir em corpus grande antes de trocar.
- [ ] **Higiene: segredo cru no corpus ingerido** 🟠 (achado colateral)
  - `programs/etoro/recon/context.txt` tinha 3 chaves de API em claro (mixpanel token/apiKey,
    googlePlacesApiKey) + e-mail da conta. Removido do índice no dedup. Ação: guarda de
    sanitização na ingestão (detectar/mascarar segredos antes de embeddar).
