# Ensaio de quatro pessoas — continuidade e economia de contexto

Execução de referência do cenário da [spec 08](../spec/08-v01-acceptance.md) com quatro pesquisadores em dois times, dois programas autorizados e troca de programa após uma semana. Produz as evidências de multi-writer (spec 00/02), de métricas (spec 09) e de integração de agentes (spec 10). Use somente programas e ativos explicitamente autorizados para os quatro participantes.

## Desenho

| | Semana 1 | Semana 2 |
| --- | --- | --- |
| Time A (2 pessoas) | programa **A** | programa **B** |
| Time B (2 pessoas) | programa **B** | programa **A** |

Hipóteses a testar:

1. **Continuidade**: na semana 2 cada time retoma o programa do outro somente com o que está no Hive, sem conversa direta na sessão de handoff.
2. **Economia de contexto**: o agente lê menos contexto pelo Hive do que leria abrindo os arquivos, sem perder o essencial.
3. **Multi-writer**: quatro pessoas escrevem no mesmo Hive sem sobrescrever, remover ou invalidar o trabalho umas das outras.

## Topologia

- **Qdrant privado** num host sempre ligado, alcançável só por VPN/rede privada com TLS válido (`docs/operations/qdrant-access.md`).
- **Quatro writers**: cada pessoa roda o binário na própria máquina com `HIVE_ROLE=writer`, `HIVE_DEVICE_ID` único (ex.: `alice-laptop`), `HIVE_WRITER_APPROVAL_ID` próprio (um change/ticket por pessoa), token `rw` individual nas duas collections, `HIVE_DATA_DIR` próprio e `HIVE_AUDIT_DIR` próprio.
- **Ollama local** em cada máquina com o mesmo modelo e digest (`validate` compara o fingerprint).
- **Pasta de dados**: recomendado um repositório git privado `hive-data` clonado por todos. Cada pessoa nomeia seus arquivos `programs/<prog>/<pasta>/<handle>-<tema>.md`; o primeiro writer que ingerir um path passa a ser o dono e os demais o ignoram (`ingest` mostra `skipped`). Commits e pushes frequentes mantêm as cópias próximas; a ingestão é sempre local a quem escreveu. Alternativa: pastas independentes, sem sincronização — funciona igualmente, mas ninguém enxerga o arquivo dos outros fora do Hive.
- **Cliente de IA**: Claude Code ou Codex em cada máquina, com o MCP `hive-mind` apontando para o binário absoluto e a skill instalada (`install-skill claude` ou `install-skill codex`).

Com isso não existe writer central: quem escreve a nota é quem a publica, e o agente da própria pessoa pode chamar `ingest_workspace`.

## Semana 0 — preparação (1 dia)

1. Operador provisiona collections, índices e quatro tokens `rw` com `sub` = `HIVE_DEVICE_ID` de cada pessoa. Registre cada emissão em `audit record`.
2. Cada pessoa: compila o binário, configura `.env.hive` (modelo em `.env.hive.example`), `ollama pull nomic-embed-text`, e executa `hive-mind ingest` uma vez com a pasta ainda vazia de notas para registrar o writer. Em seguida `hive-mind validate` deve passar com `writer_registration` própria.
3. Uma pessoa por programa cria `programs/<prog>/scope.json` e `rules.md` a partir da política real do programa, ingere e aprova: `hive-mind scope approve <prog>` (revisar hash) e depois `--yes`. Quem aprova é o dono da aprovação; os demais não precisam do `scope.json` local, mas se o tiverem por git, ele não interfere.
4. Todos instalam a skill do seu cliente e fazem uma **sessão de calibração** de uma hora: escrever uma nota no formato, `ingest_workspace`, `get_sync_status`, `hive_search` pela própria tag e `hive_get_context` para um ativo do escopo. Corrija front matter até todos verem as notas uns dos outros.
5. Definir as **tarefas-controle** de medição (seção abaixo) e anotar tokens de sessão de uma execução sem o Hive para cada uma.
6. Rodar `python3 scripts/hive_backup.py backup` como ponto zero.

## Semana 1

- Time A trabalha só em A, time B só em B. Acordo explícito: não consultar o programa do outro. A auditoria (`device_id` × `program_id`) permite verificar depois.
- Cada pessoa registra ao fim do dia, em `notes/<handle>-diario-<data>.md`: sessões, tokens do cliente (Claude Code: `/cost`; Codex: contador da sessão), o que buscou no Hive e o que faltou encontrar.
- Regra de ouro: hipóteses **descartadas** também viram nota, com `## Status: discarded` e o motivo.

## Troca — dia 8

Sem mexer no servidor. Cada time abre uma **sessão de handoff** com o agente, sem falar com o outro time:

1. `hive_get_context` para cada ativo do `scope.json` do novo programa.
2. `hive_search` com `tags: ["team-<outro>"]` e `document_types: ["note", "evidence"]`.
3. O agente escreve `notes/<handle>-handoff-<data>.md` com: estado por ativo, hipóteses abertas, hipóteses descartadas, lacunas percebidas.

Guardar o tempo gasto nessa sessão e o número de consultas (aparece em `audit report`).

## Semana 2

Igual à semana 1, no programa trocado. Ao fim, cada time responde por escrito:

- O que do trabalho do outro time foi reaproveitado sem retrabalho?
- O que faltava no Hive e teve de ser refeito ou perguntado?
- Alguma hipótese repetida por não estar registrada?
- Alguma nota de outro time ignorada por estar em formato ruim?

## Medição

### Economia bruta (auditoria)

Em cada máquina, ao fim de cada semana:

```bash
./bin/hive-mind audit report --since=<AAAA-MM-DD> > relatorio-<handle>-semana<n>.json
```

`totals.source_to_response_ratio` = bytes dos documentos de origem ÷ caracteres entregues ao agente. Consolide os quatro relatórios somando `response_chars` e `source_bytes`. Tokens ≈ `response_chars / 4`.

### Economia real (cliente)

Duas ou três tarefas fixas, definidas na semana 0, executadas por cada pessoa nos dois modos:

- **Sem Hive**: MCP desligado; o agente lê os arquivos de `hive-data/programs/<prog>` diretamente.
- **Com Hive**: MCP ligado; a skill instruindo o uso de `hive_get_context`/`hive_search`.

Compare tokens de entrada da sessão em cada modo. Exemplos de tarefa: "resuma o estado do ativo X e proponha o próximo passo", "quais endpoints com upload já foram vistos e o que foi testado".

### Conformidade e continuidade

- `audit report --since=<semana 1>` sem `--program`: linhas com `device_id` de um time e `program_id` do outro indicam consulta fora do acordo.
- Contagem de notas com a tag do outro time devolvidas nas sessões da semana 2 (a partir dos diários).
- Respostas qualitativas da semana 2.

## Registro da evidência

Ao terminar, guarde em `docs/operations/` um relatório sanitizado (sem nomes de ativos reais, sem conteúdo de notas): relatórios de `audit report` consolidados, tabela de tokens com/sem Hive, respostas qualitativas, incidentes (colisões de path, `skipped`, aprovações invalidadas) e a versão/commit usados. Esse relatório é a evidência dos gates `two_machine_acceptance` e `writer_reader_validation` e do `executed_at` das specs 08 e 09.

## Riscos e o que observar

- **Colisão de path**: duas pessoas criando o mesmo nome de arquivo. O segundo vê `skipped` e o evento `document_skipped` na auditoria. Prefixo com handle evita.
- **Aprovação invalidada**: só o aprovador invalida ao editar seu `scope.json`; se acontecer, `scope approve --yes` de novo. Trocar a política do programa no meio do ensaio distorce a comparação.
- **Latência de indexação**: a nota só existe no Hive depois do `ingest_workspace`/watcher da própria pessoa; `get_sync_status` antes de reclamar.
- **Limites**: 5 MiB por arquivo, 20 resultados por consulta, `HIVE_CONTEXT_MAX_CHARS` (12000 por padrão). `truncated` alto no relatório indica ajuste necessário.
- **Segredos em notas**: proibido; a skill avisa, mas revise antes do push.
