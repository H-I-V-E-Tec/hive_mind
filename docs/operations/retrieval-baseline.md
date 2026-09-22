# Linha de base de recuperação — 16/09/2026

Execução do harness offline ([contrato](../spec/contracts/eval-report.md)) sobre o corpus sintético `server/testdata/eval/` com Qdrant em memória com scoring real (cosseno, produto escalar esparso e RRF) e um embedder sintético bag-of-words de 64 dimensões. Não houve acesso ao acervo privado, a Ollama nem a Qdrant.

**O que estes números medem:** efeitos estruturais do pipeline atual — repetição de front matter nos chunks, dominância de um documento no top-k, custo de embeddings por chunk, idempotência da reingestão e o filtro de classificação da busca. **O que não medem:** a qualidade do modelo de embeddings real. Os valores de Recall@k e MRR abaixo descrevem o embedder sintético e servem como sinal de regressão do harness, não como medida do `EMBEDDING_MODEL` configurado em produção.

## Corpus e consultas

13 arquivos (10 Markdown com front matter, 2 TXT sem metadados, 1 JSON) em `programs/demo/` e `programs/other/`; 11 consultas rotuladas, todas do programa `demo`. Inclui um relatório longo com oito seções (`long-report.md`) que compartilha vocabulário com os demais documentos, e um par quase idêntico (`note-recon-dns.md` / `note-recon-dns-copy.md`).

## Ingestão

| Medida | Resultado |
| --- | --- |
| Arquivos / chunks | 13 / 29 |
| Bytes de conteúdo indexado | 11.056 |
| Bytes de prefixo repetido (front matter + título) | 3.802 (**34,4%** do conteúdo) |
| Chamadas de embedding / bytes de prompt | 30 / 11.081 (29 chunks + 1 sondagem de dimensão) |
| Reingestão sem mudanças: chamadas de embedding | **0** |
| Tempo de parede (1ª ingestão / reingestão) | 140 ms / 1 ms, máquina de desenvolvimento, sem valor comparativo |

## Recuperação (`limit=8`, 11 consultas)

| Modo | Recall@1 | Recall@3 | Recall@8 | MRR | Docs distintos no top-8 (média) | Maior fração de um só documento (média) |
| --- | --- | --- | --- | --- | --- | --- |
| dense | 0,636 | 0,818 | 0,909 | 0,795 | 4,64 | 0,41 |
| sparse | 0,818 | 0,909 | 0,909 | 0,909 | 4,18 | 0,38 |
| hybrid (RRF) | 0,818 | 0,909 | 0,909 | 0,909 | 3,73 | 0,53 |

Os modos `sparse` e `hybrid` foram avaliados definindo `SearchMode` diretamente no worker; a configuração de produção continua fixa em `dense` e não expõe a escolha (item da Fase 1).

## Observações estruturais

1. **Documentos sem `classification` nunca são devolvidos.** A consulta `q08` (rótulo `procedure-backup.txt`) falhou nos três modos, e `hosts.txt` não aparece em nenhuma resposta: arquivos TXT e JSON sem o campo recebem `classification: unknown`, e o filtro obrigatório da busca só aceita `internal` (ou `internal`+`restricted`, conforme `HIVE_MAX_CLASSIFICATION`). O comportamento é fail-closed por projeto (spec 04), mas o custo de embedding é pago e o conteúdo fica inacessível sem aviso ao usuário. Recall@8 máximo possível neste corpus é, portanto, 10/11 = 0,909.
2. **Um documento longo domina o top-k.** `long-report.md` ocupou 8/8 resultados em `q09` (`sparse`, `hybrid`) e 7/8 em `q07` (`dense`). `deduplicateSearchPoints` só remove o mesmo chunk; não há cota por documento.
3. **Um terço do texto indexado é prefixo repetido.** `chunkMarkdown` antepõe front matter e título a cada seção; em documentos com muitas seções curtas isso domina o embedding e o orçamento de resposta.
4. **Reingestão é idempotente** no nível de arquivo: revisão inalterada não gera chamadas de embedding.
5. **Isolamento por programa** manteve-se: nenhum resultado de `programs/other/` apareceu, embora `other/base.md` compartilhe vocabulário com `q02`.

## Reprodução

```bash
export GOCACHE=/tmp/hive-mind-go-cache GOMODCACHE=/tmp/hive-mind-modcache
go test ./server -run 'TestRetrievalBaseline' -count=1 -v          # imprime o JSON
HIVE_EVAL_WRITE=/tmp/eval.json go test ./server -run 'TestRetrievalBaseline$' -count=1
```

Dois relatórios são idênticos fora dos campos `*duration_ms` (`TestRetrievalBaselineIsDeterministic`). Para medir o modelo real, a Fase 1 deve executar o mesmo harness com transporte HTTP real e declarar `EMBEDDING_MODEL` e digest no relatório.
