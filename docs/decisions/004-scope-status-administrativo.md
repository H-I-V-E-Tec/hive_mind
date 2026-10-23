# 004 — Leitura administrativa do escopo aprovado

- Estado: aceita para incremento de integração
- Data: 2026-10-23
- Relacionada: [002](002-security-is-primary.md)

## Contexto

O hive_burp precisa do contrato `scope status` somente leitura. O Center ainda emite `programs: []`, sem concessões por programa, e o Mind não pode tratar esse campo como autorização geral de leitura de regras. A permissão `mind.scope.approve` já protege publicação e aprovação do mesmo manifesto.

## Decisão

O primeiro reader remoto de regras aprovadas usa `product.mind` e `mind.scope.approve` no servidor writer. O CLI exige `--json` e escreve somente o objeto versionado em stdout; falhas usam stderr e código não zero. Novas aprovações registram `scope_document_hash` do JSON normalizado publicado, além da revisão dos bytes originais. O servidor lê aprovação e documento publicado, valida o hash/proveniência, normaliza regras e reconsulta a aprovação antes de responder. Aprovações antigas sem o hash novo exigem reaprovação explícita. O comando não publica, aprova ou altera o estado de escopo. Resposta inválida ou grande falha fechada.

## Consequências

Esse caminho permite prova de contrato com uma identidade administrativa, sem conceder regras a membros comuns ou leitores sem programa autorizado. Não satisfaz a identidade reader nem a autorização por programa exigidas para o piloto do hive_burp. A futura API de concessões do Center e seu enforcement no Mind exigem decisão e testes próprios; o modo administrativo não é fallback para membros sem concessão.
