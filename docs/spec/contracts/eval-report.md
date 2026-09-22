# Relatório de avaliação de recuperação — versão 1

Estado: implementado como harness offline (`server/eval.go`), executado por `go test`. Não há subcomando CLI nem ferramenta MCP; a avaliação contra Qdrant/Ollama reais está prevista na Fase 1 do plano de melhoria e deve reutilizar estes tipos.

## Entrada: consultas rotuladas

Arquivo JSON com uma lista de consultas. Cada consulta nomeia o programa e os paths relativos que contam como relevantes; paths fora de `programs/<program_id>/` são rejeitados na carga, para que um rótulo nunca cruze programas.

```json
[
  {
    "id": "q04",
    "program_id": "demo",
    "query": "SSRF buscador de imagens metadados instância Andorinha",
    "relevant": ["programs/demo/evidence-ssrf.md"]
  }
]
```

## Saída: `EvalReport`

| Campo | Conteúdo |
| --- | --- |
| `schema_version` | `1`. |
| `ks` | Valores de k avaliados, na ordem pedida. |
| `ingestion` | Custo de publicar o workspace uma vez e reingerir sem mudanças. |
| `retrieval[]` | Um bloco por modo de busca (`dense`, `sparse`, `hybrid`), na ordem executada, sobre o mesmo índice. |

### `ingestion`

| Campo | Significado |
| --- | --- |
| `files`, `chunks` | Arquivos ingeríveis sob `programs/` e chunks produzidos pelo parser atual. |
| `content_bytes` | Soma dos bytes de texto dos chunks. |
| `repeated_prefix_bytes` | Para cada documento com mais de um chunk, bytes do prefixo comum a todos os chunks × (chunks − 1). Mede front matter e título repetidos por seção. |
| `repeated_prefix_ratio` | `repeated_prefix_bytes / content_bytes`. |
| `embedding_calls`, `embedding_prompt_bytes` | Chamadas de embedding bem-sucedidas e bytes de prompt na primeira ingestão, incluindo sondagens de infraestrutura. |
| `duration_ms` | Tempo de parede da primeira ingestão. Varia por máquina; não é comparado entre execuções. |
| `reingest_embedding_calls`, `reingest_duration_ms` | Mesma medida na segunda ingestão; a primeira deve ser `0`. |

### `retrieval[]`

| Campo | Significado |
| --- | --- |
| `mode`, `limit`, `queries` | Modo avaliado, `limit` passado a `hive_search` e número de consultas. |
| `recall_at_k` | Para cada k, média sobre as consultas da fração de documentos relevantes presentes entre os k primeiros resultados. Resultados são chunks; um documento pode ocupar vários. |
| `mrr` | Média de `1 / posição do primeiro resultado relevante`; `0` quando não encontrado. |
| `mean_distinct_documents` | Média de documentos distintos entre os resultados devolvidos. |
| `mean_max_document_share` | Média da maior fração dos resultados ocupada por um único documento. |
| `embedding_calls` | Chamadas de embedding feitas pelas consultas (uma por consulta em todos os modos atuais). |
| `duration_ms` | Tempo de parede das consultas. |
| `results[]` | Por consulta: `id`, `returned`, `first_relevant_rank` (0 = ausente), `distinct_documents`, `max_document_share`, `paths` na ordem devolvida. |

## Reprodutibilidade

- Dois relatórios da mesma entrada devem ser idênticos fora dos campos `*duration_ms`; `TestRetrievalBaselineIsDeterministic` verifica isso.
- O relatório nunca inclui texto dos chunks, hashes ou caminhos absolutos; apenas paths relativos ao workspace avaliado.
- O harness não sabe qual modelo respondeu `/api/embeddings`. Um relatório só descreve qualidade de modelo quando quem o publica declara o modelo usado; a [linha de base atual](../../operations/retrieval-baseline.md) usa um embedder sintético e mede somente efeitos estruturais do pipeline.
