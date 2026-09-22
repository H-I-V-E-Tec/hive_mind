# Corpus sintético para avaliação de recuperação

Nenhum dado foi extraído do acervo privado. Hosts usam o domínio reservado `example.test`; nomes de equipes, ferramentas e achados são fictícios.

`corpus/` é copiado para um workspace temporário pelo teste `TestRetrievalBaseline` (`server/eval_test.go`) e publicado em um Qdrant em memória que pontua de verdade (cosseno, produto escalar esparso e RRF). O embedder é um bag-of-words hasheado de 64 dimensões (`server/eval_fake_test.go`): determinístico e discriminativo o bastante para ordenar estes arquivos, mas sem relação com o modelo real.

`queries.json` contém 11 consultas do programa `demo` com os paths relevantes. `q05` e `q11` têm dois documentos relevantes; `q08` aponta para um TXT sem front matter e, por isso, nunca é encontrada — o corpus preserva o caso de propósito. `programs/other/base.md` compartilha vocabulário com `q02` para verificar isolamento por programa.

Resultados e leitura: [docs/operations/retrieval-baseline.md](../../../docs/operations/retrieval-baseline.md). Contrato do relatório: [docs/spec/contracts/eval-report.md](../../../docs/spec/contracts/eval-report.md).
