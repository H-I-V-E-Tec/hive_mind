# Segurança 05 — Proteção e isolamento dos dados

## Objetivo

Evitar mistura, exposição excessiva e retenção indefinida de dados de recon.

## Requisitos

- Cada ponto recebe `hive_id`, `program_id`, `classification`, `claimed_scope_status`, `effective_scope_status`, `document_id` e `document_revision`; valores ausentes não ganham autorização implícita.
- Toda consulta inclui filtro de `hive_id` definido pelo servidor. Busca e contexto exigem `program_id`; a descoberta global de alvos enumera apenas programas com escopo aprovado e verifica cada revisão ativa antes de reunir resultados.
- Collection exclusiva por Hive na v0.1. Reutilização por outro Hive é proibida.
- Dados locais, volume Qdrant e backups usam criptografia em repouso fornecida pelo sistema/infraestrutura.
- O perfil de implantação define retenção numérica por programa e como a expiração alcança arquivo, vetores derivados, tombstones, logs e backups.
- Respostas retornam o mínimo de conteúdo necessário e aplicam `HIVE_MAX_CLASSIFICATION`. Isso é minimização na aplicação, não controle de acesso do Qdrant por payload.

## Integridade

Hashes ligam chunks ao arquivo ingerido. Uma revisão só se torna ativa depois que todos os pontos forem confirmados; consultas conferem o registro ativo da collection de controle. Atualizações e exclusões são recuperáveis por revisão/tombstone, e o reconciliador limpa pontos inativos posteriormente. Caminho e fonte permitem verificar cada resultado.

## Aceite e testes

- Testes negativos tentam cruzar Hive, collection e programa.
- Exclusão grava tombstone antes de ocultar/remover todos os pontos do documento e gera evento de auditoria sem conteúdo.
- Restauração de backup preserva isolamento e metadados.
