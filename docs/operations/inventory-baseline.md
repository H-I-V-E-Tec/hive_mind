# Linha de base do inventário — 15/09/2026

Execução do novo comando em cópia privada e estável de `hive-data/`, criada em diretório temporário com acesso restrito. Não houve ingestão, geração de embeddings, alteração dos originais ou escrita em collections. O relatório abaixo contém somente agregados.

| Medida | Resultado |
| --- | --- |
| Arquivos regulares | 100 |
| Bytes observados e lidos | 25.343.943 |
| Arquivos com hash verificado | 100 |
| Arquivos vazios | 16 |
| Acima do limite de 50 MiB | 0 |
| Entradas ignoradas / falhas | 0 / 0 |
| Grupos de SHA-256 repetido dentro de programa | 1 |
| Cópias excedentes nesse grupo | 15 |
| Bytes repetidos | 0 |

Distribuição: 4 JSON (5.333 bytes), 29 Markdown (248.290 bytes) e 67 TXT (25.090.320 bytes).

O único grupo encontrado reúne arquivos vazios. Não foram encontradas cópias integrais não vazias dentro do mesmo programa. Isso não exclui trechos repetidos, documentos com formatação diferente, metadados distintos ou paráfrases: esses casos não são medidos por esta etapa.

## Reprodução

Compilar a revisão com o comando `go build -o /caminho/hive-mind .` e executar:

```bash
/caminho/hive-mind inventory /caminho/da/copia-do-hive-data
```

Não carregar tokens ou iniciar serviços. A execução de validação também passou com `HIVE_MODE=invalid`, confirmando que esse diagnóstico não usa a configuração de serviços legada/atual.

Para uma amostra versionada, reproduzível independentemente do corpus privado:

```bash
/caminho/hive-mind inventory server/testdata/inventory/corpus --details
```

A amostra sintética contém seis arquivos e cinco pares rotulados. A linha de base de recuperação (Recall@k/MRR), o custo de embeddings e a similaridade semântica continuam pendentes; estes números não representam qualidade de busca nem quantidade de conhecimento único.
