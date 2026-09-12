# 09 — Métricas de uso e economia de contexto

## Resultado

O operador mede, sem inspecionar conteúdo, quanto contexto o Hive entregou aos agentes e quanto eles teriam de ler sem ele, por dispositivo, programa, ferramenta e dia.

## Motivação

A tese do produto é reduzir o contexto que um agente precisa carregar para agir com segurança. Sem medição essa tese não é verificável. A auditoria já registra cada consulta com dispositivo, programa, quantidade e duração; esta spec acrescenta tamanho, mantendo a regra da [segurança 07](security/07-audit-and-privacy.md): números, nunca texto.

## Comportamento

Cada evento `hive_search` e `hive_get_context` registra, além dos campos existentes:

| Campo | Regra |
| --- | --- |
| `response_chars` | Quantidade de caracteres (runes) da resposta serializada em JSON que a ferramenta devolveu ao agente. É o que o agente efetivamente leu. |
| `source_bytes` | Soma do tamanho original (`document_bytes` no head) dos documentos **distintos** que originaram os itens devolvidos. Aproxima o que o agente teria de carregar para obter a mesma informação lendo os arquivos. Documentos indexados antes deste campo contribuem com `0`. |
| `truncated` | Verdadeiro quando a resposta foi cortada por limite de segurança ou orçamento de contexto. |

Buscas internas emitidas por `hive_get_context` não geram evento `hive_search` próprio; o evento do contexto carrega o agregado. Falhas registram `outcome=failed` com os contadores disponíveis.

O head de cada documento passa a conter `document_bytes` (tamanho do arquivo lido pelo writer), preservado por todas as reescritas do head. O valor é somente uma métrica: não participa de revisão, hash, escopo ou filtro.

## Comando `audit report`

`hive-mind audit report [--since=AAAA-MM-DD] [--program=<program_id>]` lê todos os `audit-*.jsonl` de `HIVE_AUDIT_DIR` do dispositivo e imprime JSON com:

- `rows`: uma linha por dia UTC × `device_id` × `program_id` × ferramenta, com `queries`, `failed`, `truncated`, `results`, `response_chars`, `source_bytes`, `duration_ms` e `source_to_response_ratio` (`source_bytes / response_chars`);
- `totals`: os mesmos contadores consolidados;
- `files`, `events` e `notes` explicativas, incluindo a aproximação `tokens ≈ response_chars / 4`.

Linhas ilegíveis são contadas e nunca ecoadas. O comando não acessa Qdrant nem Ollama e não altera os logs. Como cada dispositivo audita localmente, a consolidação entre dispositivos é feita pelo operador juntando os relatórios.

## Interpretação

- `source_to_response_ratio` é a **economia bruta**: quantas vezes menor foi o contexto entregue em relação aos documentos completos. Não considera o custo do próprio protocolo MCP nem tarefas em que o agente não usaria os arquivos.
- A **economia real** exige comparar tokens de sessão do cliente, com e sem o Hive, em tarefas fixas; o Hive fornece o denominador auditável, não o experimento.
- `truncated` alto indica orçamento (`HIVE_CONTEXT_MAX_CHARS`, `limit`) pequeno demais ou notas grandes demais.

## Segurança

Nenhum campo novo contém texto. `audit report` só lê arquivos do diretório de auditoria, dentro de `os.Root`, e respeita as permissões existentes. Identificadores de dispositivo e programa continuam sensíveis: o relatório é para o operador, não para agentes.

## Aceite e testes

- Uma busca com itens de dois documentos registra `response_chars` igual ao tamanho serializado da resposta e `source_bytes` somando cada documento uma única vez.
- `hive_get_context` gera exatamente um evento com o agregado e nenhum `hive_search` adicional.
- Fixtures com sentinelas confirmam que consulta, pergunta e trechos não aparecem no log nem no relatório.
- O relatório agrega, ordena, filtra por data e programa, rejeita flags inválidas e conta linhas ilegíveis sem ecoá-las.
