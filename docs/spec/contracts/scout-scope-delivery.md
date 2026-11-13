# Entrega de escopos pelo Scout

Estado: implementado e validado localmente com serviços descartáveis; deploy pendente. Data: 2026-11-13.

O Scout pode usar `POST /api/v1/documents/ingest` com `document_type=scope`,
`source_format=json` e um manifesto v1 canônico no campo `content`.
O manifesto inclui `platform=hackerone`, `target_name=@program` e
`classification=internal` para fonte explicitamente pública, ou `restricted`
para fonte privada ou de privacidade desconhecida. A classificação externa do
pedido coincide com a do manifesto. Conteúdo, limites e concessões seguem
[a ingestão remota](remote-ingestion.md).

`mind.scope.publish` mais `product.mind` e concessão de contribuição no programa
autorizam somente a publicação. A permissão depende de `product.mind` no Center.
As credenciais automatizadas não possuem `mind.scope.approve`; rotas de aprovação
e prévia administrativa continuam exigindo essa permissão. Publicação não aprova
escopo, e mudança invalida a aprovação anterior conforme o contrato existente.
Tokens humanos com `mind.scope.approve` continuam podendo publicar.

O Scout só confirma entrega com HTTP 200 e relatório v1 com `ok=true`,
`exit_code=0` e exatamente um resultado do path canônico do programa,
`published=true` com outcome `created`/`updated`, ou outcome `unchanged`.
Mantém pedido privado durável antes do envio e repete os mesmos bytes após
resposta perdida. O recibo local registra o hash exato do manifesto enviado;
não substitui a confirmação explícita de aprovação humana.

Esta entrega direta usa configuração do worker e credencial curta por membro,
renovada fora do Scout. Não consome a fila web SPEC-028 do Center, não implementa
renovação automática nem revogação imediata de JWT já emitido. O teste integrado
usa fonte sintética, HTTP/JWT real e Qdrant/Ollama descartáveis; homologação em
ambiente implantado permanece separada.
