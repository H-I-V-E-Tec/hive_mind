# Plano de melhoria do serviço — Hive Mind

Baseado no benchmark de 2026-09-12 (dedup + Hive vs grep no corpus etoro) e nos bugs
registrados no `TODO.md` (seção "🐞 Bugs encontrados em teste de uso real"). Amarrado ao
objetivo real: **4 pessoas continuando o contexto uma da outra**, não recall single-user.

## O que o benchmark mostrou (e o que ele NÃO mede)

- Single-user, corpus próprio pequeno (~18 docs, bem-nomeados): **grep ganhou** do Hive —
  hit@3 8/8 vs 7/8, MRR 0,875 vs 0,710, ~2× menos tokens. Hive errou até uma paráfrase (SSRF→IMDS).
- Dedup melhorou **precisão e higiene**, mas **não** baixou tokens (+3%): custo é função do `limit`.
- **Ressalva central:** esse teste é o pior caso do vetor (corpus pequeno, keyword-rico) e não
  mede a tese do projeto (handoff multi-writer). O valor do Hive está em escala + colaboração.

**Meta do plano:** (1) tornar o retrieval competitivo já em corpus pequeno; (2) provar o ganho
real no cenário multi-usuário; (3) endurecer para operação de 4 pessoas.

---

## Fase 0 — Correção & robustez (destrava o uso multi-usuário) 🔴

Sem isso, um índice parcial/velho mata a confiança do time.

| # | Problema | Ação | Métrica de sucesso |
|---|----------|------|--------------------|
| 0.1 | `ingest_workspace` **aborta** no 1º arquivo grande (scan global) | Pular (warn) arquivos > limite e seguir; listar pulados no summary | Ingest completa com N arquivos, 0 abortos; pulados reportados |
| 0.2 | Watcher **não purga** vetores no remove/move (macOS) | Corrigir fsnotify Remove/Rename; reconciliar órfãos no `ingest` | Mover/remover arquivo some da busca sem `remove` manual |
| 0.3 | Erros operacionais **mal classificados** (arquivo grande → "service unavailable") | Buckets/mensagens dedicados (uso/config vs conectividade) | Mensagem aponta a causa real |

## Fase 1 — Qualidade de retrieval (fecha o gap vs grep) 🔴 *maior alavanca*

| # | Problema | Ação | Métrica |
|---|----------|------|---------|
| 1.1 | **Hybrid (BM25+denso) marcado feito mas inativo** no `hive_search` | Fundir sparse+denso no ranking (RRF ou weighted); expor peso | Hive ≥ grep em hit@3/MRR no corpus etoro; some o miss da paráfrase |
| 1.2 | **1 doc domina o top-k** (7/8 = mesmo arquivo) | Cap por-documento (ex.: máx 2 chunks/doc; melhor-chunk + expandir sob demanda) | ≥ 4 docs únicos no top-8; recall de docs distintos ↑ |
| 1.3 | Sem reordenação fina | Reranker cross-encoder no top-N | MRR ↑ vs Fase 1.1 |
| 1.4 | Embedding com lacuna de domínio (`nomic-embed-text`) | Avaliar modelo maior/de código; medir antes de trocar | hit@3 em queries parafraseadas ↑ |

**Gate da Fase 1:** re-rodar o benchmark (mesmo harness) e o Hive **empatar ou superar** o grep.

## Fase 2 — Economia de token (real, não cosmética) 🟡

| # | Problema | Ação | Métrica |
|---|----------|------|---------|
| 2.1 | Custo = função do `limit`, não do conteúdo | Budget de tokens por chamada + `limit` default menor | tokens/consulta ↓ sem perder hit |
| 2.2 | Frontmatter YAML repetido em cada chunk | Enxugar metadados na resposta (só essencial) | `response_chars` ↓ ~10-15% |
| 2.3 | Cap por-doc (1.2) também economiza | (medir junto) | `source_to_response_ratio` melhora |

## Fase 3 — Validação multi-usuário (a tese do projeto) 🔴

Rodar o `four-person-trial.md` com o setup de `multi-machine-setup.md`.

| Hipótese | Como medir | Sucesso |
|----------|-----------|---------|
| **Continuidade** (retomar o programa do outro só pelo Hive) | Sessão de handoff dia 8; % de estado reconstruído sem conversar | Time reconstrói estado + próximos passos sem perguntar |
| **Economia real** | Tarefas fixas com/sem Hive; tokens de sessão (`/cost`) | Com-Hive ≤ sem-Hive, sem perder essencial |
| **Multi-writer sem colisão** | `audit report` por device×program; eventos `skipped` | 0 sobrescritas; ownership respeitado |

**Este é o teste que decide se o hive-mind é o core** — os números single-user não decidem.

## Fase 4 — Endurecimento & operação 🟠

| # | Ação |
|---|------|
| 4.1 | **Guarda de sanitização na ingestão** — detectar/mascarar segredos (chaves, tokens, e-mails) antes de embeddar (achamos 3 chaves em claro no corpus) |
| 4.2 | **Backoff/retry** de conexão Qdrant/Ollama (já no TODO) |
| 4.3 | Persistência dos serviços (launchd/brew services) no host do Qdrant |
| 4.4 | Rotação/revogação de JWT documentada e testada (`qdrant-access.md`) |
| 4.5 | Backup agendado (`scripts/hive_backup.py`) do host do Qdrant |

---

## Ordem recomendada

1. **Fase 0** (destrava ingest confiável) →
2. **Fase 1.1 + 1.2** (hybrid + cap: o maior salto de qualidade) →
3. **Fase 3** (rodar o ensaio de 4 — valida a tese) →
4. Fases 2 e 4 conforme o ensaio expõe prioridades.

> Regra: cada fase fecha com **re-medição** (benchmark harness / `audit report`), pra o ganho
> ser número, não impressão. Os scripts do benchmark estão no scratchpad da sessão de 2026-09-12
> (`nohive_bench2.py`, `sem_rank2.py`) — portá-los pra `tests/` ou `scripts/` como suíte fixa.
