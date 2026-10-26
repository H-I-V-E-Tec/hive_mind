# Autorização por programa no Mind remoto

Estado: emissor no Center, validação no Mind e portal administrativo implementados localmente; homologação integrada e rollout pendentes. O rollout exige migrar a API do Center e configurar concessões explícitas antes de atualizar o servidor Mind.

O contrato v1 do JWT mantém `programs: string[]` como programas de leitura e
acrescenta `program_access_version: 1` e `program_contribute: string[]`. As listas
são ordenadas, únicas, com até 128 IDs canônicos; contribuição é subconjunto da
leitura. Ausência de versão, lista inválida ou programa não concedido nega o
caminho remoto afetado. Permissões globais continuam exigidas em conjunto.

## Contrato necessário

- O Center mantém concessões explícitas por membro e programa, com ao menos
  leitura e contribuição separadas. Membro inativo não recebe acesso efetivo.
- O token assinado informa a versão do contrato e os programas concedidos. Lista
  vazia significa nenhum programa; ausência da versão não pode virar acesso
  irrestrito após a migração. Concessões de escopo exigem permissão administrativa
  adicional.
- O Mind verifica a concessão em **todas** as rotas por programa: busca,
  contexto, catálogo filtrado, publicação de nota/evidência, publicação e
  aprovação de manifesto. `program_id` recebido do cliente nunca é uma concessão.
- A descoberta global de alvos só varre os programas concedidos antes de contar,
  ranquear e paginar; não devolve nomes, caminhos, totais ou avisos derivados de
  programas alheios. O limite atual de varredura continua explícito.
- Tokens atuais expiram em até 12 horas. Revogar membro/programa no Center impede novos tokens, mas os já emitidos continuam válidos até expirar; a revogação imediata não está implementada e precisa de aceite de risco ou redução da janela antes de produção. O filtro de classificação e a aprovação de escopo continuam
  controles distintos da autorização do membro.

## Aceite cruzado antes de ativar

Com dois membros e dois programas sintéticos, cada membro consegue ler e
contribuir apenas no programa concedido. Busca direta, contexto, catálogo global,
ingestão e aprovação tentados no outro programa retornam autorização negada sem
divulgar conteúdo ou existência do programa. Remover a concessão impede novos
pedidos dentro do prazo de revogação publicado. O rollout do Center deve ser
atualizado antes do servidor Mind; `programs: []` nunca é concessão global.
