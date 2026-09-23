# Conversão e ingestão padronizada

O fluxo disponível é **arquivo ou stdin → extração determinística → envelope `hive-document/v1` → blocos → chunks → embeddings locais → publicação verificada no Qdrant**. A conversão não usa um LLM para reescrever os fatos. O significado dos textos, valores numéricos e negações deve ser preservado; a busca semântica é feita pelos embeddings.

## Cobertura

| Entrada | Extração |
| --- | --- |
| Markdown e TXT UTF-8 | Texto e seções com referência à origem. |
| JSON | Estrutura determinística, preservação de números e localizadores JSON Pointer. |
| JSONL / NDJSON | Registros individuais, com limite agregado de elementos. |
| CSV / TSV | Primeira linha como cabeçalho; registros com nomes das colunas e células preservados, incluindo campos entre aspas e multilinha. |
| stdin | Um dos formatos anteriores, declarado com `--format`. |

PDF, documentos Office, HTML, arquivos compactados, áudio, vídeo e imagens não têm adaptador ativo. É necessário extraí-los previamente para um formato aceito; OCR e transcrição exigem ferramentas próprias e revisão de fidelidade. Renomear um binário para `.txt` não o torna suportado. O código legado de extração PDF não participa deste fluxo.

O padrão permite integrar exports de APIs, logs e ferramentas sem alterar o publicador vetorial. O suporte a um novo formato exige um adaptador com validação, limites e testes, além de localizadores confiáveis. Não há promessa de interpretar qualquer formato arbitrário.

## Preparar uma importação

Compile o binário e crie a pasta de destino. Os exemplos pressupõem a raiz do repositório; `acme` deve ser um programa real de sua instalação.

```bash
go build -trimpath -o bin/hive-mind .
mkdir -p hive-data/programs/acme/imports
./bin/hive-mind convert /caminho/export.csv \
  --program=acme \
  --platform=h1 \
  --target='API Service' \
  --classification=internal \
  --document-type=evidence \
  --source=export-autorizado \
  --tag=recon \
  --asset-ref=api.example.com \
  --output=hive-data/programs/acme/imports/alice-export.json
```

`convert` funciona sem Qdrant, Ollama, tokens ou configuração Hive. O arquivo de saída é privado (`0600`) e publicado inteiro; se o destino já existir, o comando falha sem sobrescrevê-lo. Use um caminho novo para uma nova fonte. Para atualizar a mesma fonte, revise o novo envelope fora da pasta observada e substitua o arquivo existente de forma atômica, preservando o path; a próxima ingestão publicará a nova revisão. Nunca direcione stdout com `>` para um arquivo já observado enquanto o watcher estiver ativo.

Sem `--output`, a saída é o envelope JSON em stdout para inspeção. Ela **contém o conteúdo da fonte** e não deve ir para logs compartilhados. Erros vão para stderr sem trechos da entrada. Os argumentos opcionais aceitam `--nome=valor`; `--tag`, `--asset-ref` e `--observed-target` podem se repetir. `--collected-at` aceita RFC 3339, como `2026-09-17T12:00:00Z`; não informar mantém a data de observação ausente.

Para consumir um export existente por stdin:

```bash
./bin/hive-mind convert - \
  --format=jsonl \
  --source=export-api \
  --program=acme \
  --platform=h1 \
  --target='API Service' \
  --classification=internal \
  --output=hive-data/programs/acme/imports/alice-api.json < /caminho/export.jsonl
```

Programa, classificação e tipo vêm dos argumentos do operador. Campos com os mesmos nomes dentro do export continuam sendo conteúdo, sem conceder autorização. A aprovação de escopo continua sendo um procedimento separado baseado em `scope.json`.

## Formato persistido antes do embedding

O envelope tem o marcador `hive_document_schema: "hive-document/v1"`, os metadados usuais no objeto raiz e:

- `source_format`, `raw_hash` (SHA-256 dos bytes recebidos) e `converter_fingerprint`;
- `blocks`, em ordem, com `ordinal`, `kind`, `text` e `locator`;
- `program_id`, `classification`, `document_type`, `source` e, quando fornecidos, `platform`, `target_name`, `observed_targets`, `collected_at`, `tags`, `asset_refs`.

`platform` é a plataforma (`h1`, `bugcrowd` etc.); `program_id` é o programa
específico dentro dela; `target_name` é o nome do projeto (`API Service`), não
um host. `observed_targets` contém hosts, IPs e URLs encontrados no recon,
normalizados como `host:api.example.com`, `ip:203.0.113.10` ou
`url_prefix:https://api.example.com/v1`. Linhas TXT inteiras, colunas
`host`/`hostname`/`ip`/`url`/`target` em CSV/TSV e campos equivalentes em
JSON/JSONL/NDJSON são extraídos; Markdown e prosa não são interpretados.
`--observed-target` adiciona um ativo revisado manualmente. Esses campos não
aprovam escopo: só o `scope.json` aprovado faz isso. `asset_refs` continua
separado, para ligar uma nota à busca contextual após revisão.

O envelope é o documento de entrada; não é um payload Qdrant pronto. A ingestão transforma cada bloco em chunks com os limites do writer. Só o texto útil do chunk é embedado; as chaves de transporte do envelope não entram no texto. A busca inclui `source_format`, `source_locator` e `canonical_hash` para os chunks convertidos.

Cada ponto vetorial mantém Hive, programa, classificação, revisão do documento e de escopo, path, ordinal e data de indexação, mais fingerprint do conversor, hash da origem, referência ao bloco e dispositivo writer. `canonical_hash` identifica o texto normalizado daquele chunk; **não é garantia de deduplicação global ou equivalência semântica**. Os localizadores referem-se à fonte identificada por `source`/`raw_hash`; preserve a fonte se precisar verificar os trechos no futuro. O conversor não armazena uma cópia adicional do original.

## Ingerir e verificar

Configure o writer conforme o [guia de servidor e múltiplos usuários](server-multiuser-guide.md) e carregue seu ambiente. `HIVE_DATA_DIR` precisa apontar para a pasta `hive-data` acima, por caminho absoluto.

```bash
./bin/hive-mind ingest
./bin/hive-mind ingest
./bin/hive-mind search acme "api.example.com"
```

A primeira execução deve mostrar `created` para a nova fonte.

### Converter e ingerir em um passo (`convert --ingest`)

Para evitar os dois comandos, `convert --ingest` converte a fonte e publica o
envelope no Qdrant na mesma invocação. Ele exige `HIVE_ROLE=writer` e um
`--output` **dentro de `HIVE_DATA_DIR/programs/<program_id>/`**, porque a
identidade do documento, a idempotência e a propriedade por writer continuam
baseadas no path. A conversão em si permanece offline; só a etapa de ingestão
carrega a configuração e contata Qdrant/Ollama.

```bash
./bin/hive-mind convert /caminho/export.csv \
  --program=acme \
  --platform=h1 \
  --target='API Service' \
  --classification=internal \
  --document-type=evidence \
  --source=export-autorizado \
  --output=hive-data/programs/acme/imports/alice-export.json \
  --ingest
```

A saída é o mesmo relatório JSON por arquivo do `ingest` (schema v1), com
`created` na primeira vez e `unchanged` ao repetir sem mudanças. Um `--output`
fora de `HIVE_DATA_DIR` é recusado; um reader recebe código `12`. Sem `--ingest`
o comando segue offline e apenas grava/imprime o envelope, como antes. A segunda deve mostrar `unchanged`, sem novos embeddings para esse documento. Erros individuais preservam os resultados dos outros arquivos e retornam código `15`; examine `summary.results`. A falha de uma conversão ou de uma nova revisão não troca a revisão ativa por um resultado parcial.

Para cadastrar uma fonte nova respondendo a perguntas no terminal:

```bash
./bin/hive-mind convert /caminho/hosts.txt --interactive --ingest
```

O comando pergunta plataforma, programa, tipo do arquivo, nome do alvo/projeto
e classificação. Para listas de recon, escolha `asset` ou `endpoint`; para
anotações, `note`; para provas, `evidence`. A
origem (`source`) assume o nome do arquivo. Sem `--output`, publica em
`HIVE_DATA_DIR/programs/<program_id>/<nome-do-arquivo>.json`; o diretório do
programa deve existir; a aprovação de escopo é separada. O destino nunca é sobrescrito.
Em scripts/CI, passe `--platform`, `--program`, `--target`,
`--document-type`, `--classification` e `--output` explicitamente. O watcher e o MCP não fazem
perguntas por stdin: eles apenas ingerem o registro persistido. Arquivos
históricos sem esses campos continuam legíveis, mas exigem revisão para entrar
no catálogo completo.

CLI, watcher e ferramenta MCP `ingest_workspace` usam o mesmo pipeline. CSV/TSV/JSONL/NDJSON brutos também são selecionados pela ingestão, porém ficam com `classification: unknown`, fora da busca. O relatório agora avisa `classification_unknown`. Use `convert` com classificação explícita para uma importação pesquisável.

Os padrões são 5 MiB por arquivo, 1.000 chunks, 2.000 caracteres por chunk, sobreposição de 200 caracteres, profundidade JSON de 64 e 100.000 elementos. O envelope também precisa caber no limite de arquivo. `convert --max-file-bytes=10485760` permite aumentar seu limite até 50 MiB; configure `HIVE_MAX_FILE_BYTES` correspondente na ingestão. Divida exports grandes em fontes menores. Ajustar os parâmetros do chunker de uma collection já existente continua exigindo migração do fingerprint, como antes.

## Compatibilidade e pendências

MD/TXT/JSON comuns continuam no parser anterior para não alterar os fingerprints ou reindexar o acervo automaticamente. Para aproveitar o novo padrão nesses formatos, passe-os por `convert`. Novos formatos e envelopes têm versão própria na revisão do documento. Atualize os binários de todos os writers antes de compartilhar envelopes; readers compatíveis com o payload anterior continuam lendo o texto.

O contrato completo de [unidades canônicas](../spec/contracts/canonical-unit.md) continua separado desta entrega: PostgreSQL compartilhado, ocorrências, deduplicação transacional entre writers, fila de projeção e revisão semântica ainda não estão implementados. Não há detecção automática completa de segredos, contradições ou instruções maliciosas; revise/classifique a fonte antes de importá-la. O conteúdo recuperado continua marcado como não confiável.
