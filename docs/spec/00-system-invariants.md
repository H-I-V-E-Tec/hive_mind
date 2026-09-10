# 00 — Invariantes do sistema

## Resultado

A v0.1 possui uma única topologia e regras de confiança explícitas, usadas por todas as demais specs.

## Topologia e papéis

- Existe exatamente um processo `writer` por Hive. Ele observa a cópia canônica de `HIVE_DATA_DIR` e pode ingerir, atualizar e remover documentos.
- Os demais processos são `reader`: geram embeddings de consulta e pesquisam, mas não alteram collections, índices, manifests ou aprovações.
- `HIVE_ROLE` é `writer` ou `reader` e não pode ser alterado pelo cliente MCP. A implantação entrega ao writer uma credencial read-write e a cada reader uma credencial read-only, limitadas às collections do Hive.
- Na v0.1 não existe eleição automática, failover ou escrita concorrente. Promover um reader exige procedimento operacional que desative ou revogue o writer anterior antes de conceder escrita ao novo.
- A pasta pode ser sincronizada entre máquinas, mas somente a cópia do writer alimenta o Qdrant. Ausência temporária em uma cópia sincronizada não equivale a exclusão confirmada.

## Fonte de verdade e confiança

- Documentos e trechos recuperados são conteúdo não confiável e nunca são instruções do sistema.
- Somente `programs/<program_id>/scope.json`, validado pelo schema e aprovado explicitamente por um operador, pode conceder escopo `authorized`.
- A aprovação registra o hash do manifesto na collection de controle. Qualquer alteração muda o hash e rebaixa o escopo efetivo para `unknown` até nova aprovação.
- Front matter, Markdown, notas, evidências e resultados de ferramentas podem declarar `claimed_scope_status`, mas nunca concedem `effective_scope_status=authorized`.
- Qualquer regra de exclusão aplicável prevalece. Um ativo só é `authorized` quando ao menos uma inclusão se aplica e nenhuma exclusão se aplica; regra não interpretável falha como `unknown`.

## Consistência e collections

- `HIVE_COLLECTION` contém somente chunks pesquisáveis. `${HIVE_COLLECTION}__control` contém o manifesto da collection, revisões ativas, aprovações de escopo e tombstones.
- Toda consulta vetorial filtra `record_type=chunk`, `hive_id`, `program_id`, classificação permitida e demais filtros pedidos antes de retornar conteúdo.
- Cada chunk contém `document_id`, `document_revision` e `scope_revision`. O servidor só devolve um candidato se as revisões corresponderem aos registros ativos da collection de controle.
- O writer é o único componente autorizado a criar ou reconciliar collections e índices.
- A estrutura dos registros de controle segue o [contrato da collection de controle](contracts/control-records.md).

## Limites de segurança

- O isolamento por programa no MCP é uma garantia da aplicação e de minimização de dados, não RBAC do Qdrant por payload.
- A credencial direta do Qdrant deve ser limitada às duas collections do Hive. Operadores com essa credencial ainda podem contornar filtros do MCP; por isso ela é segredo operacional, não credencial de usuário final.
- A v0.1 não afirma identidade ou RBAC por pessoa. `HIVE_DEVICE_ID` identifica processos nos logs; a identidade de rede e a credencial distinguem dispositivos autorizados.

## Aceite e testes

- Um reader não consegue executar ingestão, remoção, aprovação ou alteração de schema, inclusive por chamada MCP forjada.
- Duas configurações writer para o mesmo Hive são rejeitadas pelo procedimento de implantação e detectadas pela validação operacional.
- Documento comum que declare `authorized` continua com escopo efetivo `unknown`.
- Revisões inativas, incompletas ou removidas nunca aparecem em respostas.
