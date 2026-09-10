# Segurança 05 — Proteção e isolamento dos dados

## Objetivo

Evitar mistura, exposição excessiva e retenção indefinida de dados de recon.

## Requisitos

- Cada ponto recebe `hive_id`, `program_id`, `classification` e `scope_status`; valores ausentes não ganham autorização implícita.
- Toda consulta inclui filtro de `hive_id` definido pelo servidor e `program_id` obrigatório.
- Collection exclusiva por Hive na v0.1. Reutilização por outro Hive é proibida.
- Dados locais, volume Qdrant e backups usam criptografia em repouso fornecida pelo sistema/infraestrutura.
- Definir retenção por programa e permitir remoção verificável de arquivo, vetores derivados e backups conforme a política aplicável.
- Respostas retornam o mínimo de conteúdo necessário e respeitam classificação.

## Integridade

Hashes ligam chunks ao arquivo ingerido. Atualizações removem pontos anteriores de forma atômica ou recuperável. Caminho e fonte permitem verificar cada resultado.

## Aceite e testes

- Testes negativos tentam cruzar Hive, collection e programa.
- Exclusão remove todos os pontos do documento e gera evento de auditoria sem conteúdo.
- Restauração de backup preserva isolamento e metadados.
