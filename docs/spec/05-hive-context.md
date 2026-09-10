# 05 — Contexto Hive compacto

## Resultado

`hive_get_context` responde a uma pergunta com regras de escopo antes de evidências, respeitando um orçamento de contexto configurável.

## Contrato

Entradas: `program_id`, `question`, filtros opcionais e `limit`. A ferramenta primeiro consulta `scope` e `rules`, depois `asset`, `endpoint`, `note` e `evidence`. A saída contém estado de escopo, limites relevantes, trechos deduplicados, caminho, fonte e data.

`HIVE_CONTEXT_MAX_CHARS` limita o resultado. O truncamento remove itens inteiros a partir dos menos relevantes e preserva cabeçalho, escopo e aviso.

## Segurança

Sem uma confirmação `authorized`, a saída deve declarar que o escopo não foi confirmado. Dados `out_of_scope` não podem ser apresentados como autorização nem usados para orientar atividade.

## Aceite e testes

- Cenários autorizado, desconhecido e fora de escopo têm resposta distinta e correta.
- Escopo aparece antes das evidências; duplicatas são removidas.
- Testes validam limite, isolamento e aviso obrigatório.
