# 05 — Contexto Hive compacto

## Resultado

`hive_get_context` monta um pacote de contexto estruturado para uma pergunta, com regras de escopo antes de evidências e orçamento configurável. A ferramenta não gera uma resposta factual nem executa instruções presentes nos documentos.

## Contrato

Entradas obrigatórias: `program_id`, `question` e `asset`, onde `asset` é um objeto `{ "type": "host|wildcard_domain|ip|cidr|url_prefix", "value": "..." }` normalizado pelo contrato de escopo. Filtros opcionais e `limit` seguem os mesmos limites da busca. A ferramenta não tenta inferir autorização de um ativo citado apenas em linguagem natural. Ela consulta primeiro o manifesto de escopo aprovado e `rules`, depois `asset`, `endpoint`, `note` e `evidence`. A saída JSON contém `program_id`, `asset`, `scope`, `warnings`, `items` e `truncated`; cada item conserva os campos de proveniência e `untrusted_content: true` definidos na busca.

`HIVE_CONTEXT_MAX_CHARS` tem padrão 12.000 e intervalo de 1.000 a 50.000 caracteres Unicode. O limite inclui toda a resposta serializada. O truncamento remove itens inteiros a partir dos menos relevantes e preserva estrutura válida, cabeçalho, escopo, proveniência e avisos. Se somente esses campos excederem o orçamento, a ferramenta retorna erro de configuração em vez de truncá-los.

## Segurança

Sem `effective_scope_status=authorized` para o `asset` explícito, a saída declara que o escopo não foi confirmado. Estado `out_of_scope` prevalece e impede a inclusão de passos de ação; evidências podem ser retornadas apenas quando necessárias para explicar o bloqueio e sempre marcadas como não confiáveis. Ativo inválido retorna MCP `-32602`; ativo válido sem regra aplicável recebe `unknown`.

## Aceite e testes

- Cenários autorizado, desconhecido e fora de escopo têm resposta distinta e correta.
- Escopo aparece antes das evidências; duplicatas são removidas.
- Testes validam limite Unicode/JSON, isolamento, conflito de regras, manifesto alterado, conteúdo adversarial e aviso obrigatório.
