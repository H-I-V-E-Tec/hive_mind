# 00 — Invariantes do sistema

## Resultado

A v0.1 possui uma única topologia e regras de confiança explícitas, usadas por todas as demais specs.

## Topologia e papéis

- Um Hive possui um ou mais processos `writer`, cada um em um dispositivo autorizado com `HIVE_DEVICE_ID` próprio, `HIVE_WRITER_APPROVAL_ID` próprio e sua própria cópia de `HIVE_DATA_DIR`. Cada writer se registra na collection de controle na primeira operação de escrita; um writer não registrado ou com aprovação divergente falha em `validate`.
- Os demais processos são `reader`: geram embeddings de consulta e pesquisam, mas não alteram collections, índices, manifests ou aprovações.
- `HIVE_ROLE` é `writer` ou `reader` e não pode ser alterado pelo cliente MCP. A implantação entrega a cada writer uma credencial read-write individual e a cada reader uma credencial read-only individual, limitadas às collections do Hive.
- **Cada documento tem exatamente um writer dono**: o dispositivo que publicou a primeira revisão. Somente o dono publica novas revisões, marca `pending_delete`, remove ou poda o documento. Outro writer que encontre o mesmo path em sua pasta o ignora, registra o evento em auditoria e continua; ele nunca falha nem sobrescreve. Não há transferência automática de propriedade na v0.1; ela exige que o dono remova o documento por tombstone e outro writer o publique.
- **Cada aprovação de escopo tem um writer dono**: o dispositivo que executou `scope approve --yes`. Somente ele valida o `scope.json` local contra o hash aprovado e invalida a aprovação quando o arquivo muda. Os demais writers usam a revisão aprovada da collection de controle e reconstroem o manifesto a partir do documento de escopo publicado; a ausência ou divergência do arquivo local em um writer não dono nunca revoga a aprovação. Qualquer writer pode executar uma nova aprovação explícita e passa a ser o dono dela.
- Escrita concorrente entre writers é serializada por registro de controle com versão otimista: dois writers que disputem o mesmo registro falham fechados sem sobrescrever. Não existe eleição, failover ou reconciliação automática entre writers.
- A pasta pode ser sincronizada entre máquinas; cada writer alimenta o Qdrant apenas com os documentos que possui. Ausência temporária em uma cópia sincronizada não equivale a exclusão confirmada.

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
- Somente writers registrados criam ou reconciliam collections e índices; a criação é idempotente e a validação do manifesto da collection é idêntica para todos os dispositivos.
- A estrutura dos registros de controle segue o [contrato da collection de controle](contracts/control-records.md).

## Limites de segurança

- O isolamento por programa no MCP é uma garantia da aplicação e de minimização de dados, não RBAC do Qdrant por payload.
- A credencial direta do Qdrant deve ser limitada às duas collections do Hive. Operadores com essa credencial ainda podem contornar filtros do MCP; por isso ela é segredo operacional, não credencial de usuário final.
- A v0.1 não afirma identidade ou RBAC por pessoa. `HIVE_DEVICE_ID` identifica processos nos logs; a identidade de rede e a credencial distinguem dispositivos autorizados.

## Aceite e testes

- Um reader não consegue executar ingestão, remoção, aprovação ou alteração de schema, inclusive por chamada MCP forjada.
- Dois writers registrados publicam documentos distintos no mesmo Hive e programa; um writer não consegue alterar, marcar para exclusão, remover ou podar documento de outro.
- Um writer sem `scope.json` local ingere em programa aprovado por outro writer, herda o escopo efetivo do manifesto aprovado e não invalida a aprovação; a alteração do arquivo no writer aprovador continua invalidando.
- Writer não registrado, ou registrado com outro `HIVE_WRITER_APPROVAL_ID`, falha em `validate`; reader exige ao menos um writer registrado.
- Documento comum que declare `authorized` continua com escopo efetivo `unknown`.
- Revisões inativas, incompletas ou removidas nunca aparecem em respostas.
