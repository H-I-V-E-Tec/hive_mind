# Catálogo de alvos do Hive Mind — v1.0.0

## O que é um alvo

Um **alvo** é o nome do projeto/organização que o operador investiga, não um
host. Todo documento novo de recon, nota ou evidência deve registrar três
valores separados:

| Campo | Exemplo | Papel |
| --- | --- | --- |
| `platform` | `h1`, `bugcrowd` | Onde o programa foi encontrado. |
| `program_id` | `acme` | Isolamento dos dados e do escopo no Hive. |
| `target_name` | `API Service` | Projeto a que o arquivo pertence. |

Um programa pode ter vários alvos; um alvo pode ter vários documentos e vários
ativos observados (`host`, `ip`, `url_prefix`). O valor desses campos varia por
arquivo. Não há inferência de plataforma, programa ou nome do projeto a partir
do nome do arquivo. `scope.json` e regras cobrem o programa inteiro e podem
registrar `target_name: "@program"`; isso não cria um alvo no ranking.

O catálogo é inventário para decidir o próximo ciclo de leitura/recon. Ele não
faz varredura e não transforma observações em autorização.

## Cadastro e ingestão

O writer oferece `convert <arquivo> --interactive --ingest`, que pergunta os
campos ausentes (plataforma, programa, tipo, nome do alvo e classificação). A origem
assume o nome do arquivo ou pode ser informada em `--source`. O envelope
persistido registra `platform`, `program_id`, `target_name` e
`observed_targets`. Em automações, os mesmos valores são argumentos explícitos:
`--platform`, `--program`, `--target`, `--document-type`,
`--classification` e `--output`.

Listas de recon podem conter vários ativos. Um arquivo mantém **um nome de
alvo** e registra **todos os ativos observados reconhecidos**. A extração é
determinística e restrita: linhas TXT inteiras que sejam hosts/IPs/URLs,
colunas `host`, `hostname`, `ip`, `url` ou `target` em CSV/TSV e campos com
esses nomes em registros JSON/JSONL/NDJSON. Markdown e texto livre não viram
ativos por suposição. `--observed-target` permite adicionar manualmente um
ativo revisado. Wildcards/CIDRs são regras de escopo, não ativos concretos do
catálogo. O parser normaliza, deduplica e limita as observações por arquivo.

Watcher e MCP não fazem perguntas por stdin. Eles leem os campos registrados.
Arquivos históricos sem cadastro permanecem legíveis, mas a ingestão avisa
`target_registration_missing`; o catálogo reporta `unregistered_files` entre
os documentos ativos visíveis para a classificação do cliente e considera sua
cobertura parcial. Arquivos com `classification: unknown` também exigem revisão,
mas não entram nessa contagem. Migração histórica exige revisão humana:
nenhum arquivo recebe H1, Bugcrowd, alvo ou classificação por heurística.

## Ferramenta MCP

`hive_list_targets` recebe `program_id` (obrigatório), `limit` (1–50), `order`
(`balanced`, `most_documented`, `needs_recon`) e `include_unconfirmed` (padrão
`false`). A resposta separa `targets` de `unconfirmed`, inclui
`scope_revision`, `candidates_evaluated`, `unregistered_files`, `warnings` e
`truncated`.

Cada alvo mostra nome, plataforma, ativos observados e o status de escopo de
cada ativo; cobertura em **documentos ativos distintos** de recon, notas e
evidências; diversidade de fontes; recência; e até três caminhos de origem.
Um arquivo com mil chunks conta uma vez. O nome do projeto nunca tem
`action_allowed: true`: apenas um ativo concreto confirmado pelo `scope.json`
aprovado pode receber essa indicação. Exclusões prevalecem sobre inclusões.
Um alvo com pelo menos um ativo autorizado entra no ranking; alvos sem nenhum
ativo autorizado aparecem apenas em `unconfirmed` quando solicitados. Fontes
ligadas somente a ativos excluídos/desconhecidos não aumentam a cobertura
rankeada. Antes de agir, use `hive_get_context` no ativo específico.

`balanced` intercala três faixas: `emerging` (recon sem notas/evidência),
`ready` (recon com contexto) e `unmapped` (ativo aprovado ligado ao alvo, mas
sem recon). `most_documented` favorece diversidade de fontes e documentos;
`needs_recon` favorece alvos com menos recon. Empates usam recência e nome do
alvo para produzir uma ordem estável. O ranking mede cobertura de informação,
não probabilidade de vulnerabilidade nem valor financeiro.

## Limites e próximos passos

- Toda leitura respeita `HIVE_ID`, `program_id`, revisão de escopo, estado ativo
  da revisão e `HIVE_MAX_CLASSIFICATION`. O catálogo não retorna texto bruto.
- A varredura do índice tem limite de 5.000 documentos (chunk ordinal zero);
  quando atingido, a resposta
  traz `truncated: true` e aviso de cobertura parcial. Um futuro índice por
  documento pode remover essa limitação em corpora grandes.
- `observed_targets` serve ao catálogo. Um ativo extraído automaticamente não
  passa a ser `asset_refs` de busca contextual sem revisão, sobretudo quando
  um arquivo mistura ativos autorizados e excluídos. Para ligar a nota ao
  `hive_get_context`, adicione `asset_refs` revisados.
- Ainda é necessário ensaiar o catálogo contra um corpus real e cadastrar os
  arquivos históricos; até lá, os resultados não são um inventário completo.
