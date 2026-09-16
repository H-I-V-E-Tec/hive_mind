# Contrato proposto para o conversor

Estado: proposta para a próxima entrega; não há novos adaptadores ativos neste documento. Base: plano de melhoria, etapas 0 e 2.

## Fronteiras de responsabilidade

1. **Entrada:** stream limitado, identificador da fonte/revisão, formato declarado e contexto de importação definido pelo operador. Hive, programa, classificação e domínio de acesso não são concedidos pelo conteúdo.
2. **Adaptador:** emite blocos com texto/estrutura e localizadores (linhas, JSON Pointer, células, páginas); informa extração parcial e versões. Não publica vetores, não altera escopo e não precisa conhecer Qdrant.
3. **Normalizador:** transforma blocos em representação determinística, conservando números, negação, ordem significativa, entidades e qualificadores. Seu fingerprint identifica o algoritmo e parâmetros.
4. **Admissão:** sanitiza dados sensíveis, verifica qualidade/proveniência e emite nova unidade, reutilização, atualização, revisão necessária ou rejeição, com motivos.
5. **Persistência:** aplica a decisão com unicidade e referências de origem. Embeddings e índice são projeções posteriores.

## Dados mínimos

| Objeto | Campos previstos |
| --- | --- |
| SourceRevision | source_id, revision, raw_hash, hive_id, program_id, access_partition, classification, source_locator |
| ExtractedBlock | source_revision, ordinal, kind, text/structured_value, locator, extraction_status, converter_fingerprint |
| CanonicalUnit | schema_version, unit_kind, normalized_content, semantic_qualifiers, canonical_hash, normalizer_fingerprint |
| AdmissionDecision | decision, reason_codes, canonical_unit_ref, provenance_ref, policy_version, reviewer_ref quando aplicável |

Não gerar IDs da unidade por path. A fonte continua tendo identidade e autoria próprias; diferentes fontes podem referenciar a mesma unidade. Limitação temporária: arquivos do inventário têm identidade por localização e hash bruto apenas para diagnóstico.

## Deduplicação e versionamento

- Chave proposta: Hive + programa + domínio de acesso + classificação + tipo de unidade + versão do normalizador + hash canônico.
- Armazenamento deve confirmar igualdade canônica e tratar colisão/concorrência atomicamente. Uma consulta ao Qdrant antes de inserir não oferece essa garantia.
- Reprocessar a mesma fonte e mesmas versões é idempotente; mudança de parser, normalização ou política pode exigir reavaliação.
- Novas versões de conteúdo/escopo não alteram silenciosamente unidades compartilhadas ou autoria.
- Inventário com SHA-256 igual é apenas candidato a cópia integral; não substitui os critérios acima.
- Paráfrase, complemento, contradição e mudança temporal não são descartados automaticamente. Casos rotulados iniciais: `server/testdata/inventory/pairs.json`.

## Próxima implementação

Começar por MD/TXT/JSON com saída de blocos localizáveis e testes de determinismo/fidelidade, mantendo o publicador atual atrás de uma interface. Adicionar JSONL/CSV/TSV após estabilizar o contrato. O modelo de dados, a chave de unicidade, a retenção e o contrato dos adaptadores estão em [canonical-unit.md](canonical-unit.md); a persistência compartilhada e a garantia entre writers estão na [decisão 003](../../decisions/003-canonical-persistence.md). Migração do acervo permanece pendente e segue a seção 13 do plano.
