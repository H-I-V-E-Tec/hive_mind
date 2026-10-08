# Autorização por programa no Mind remoto

Estado: contrato de integração; **não implementado**. O emissor atual do Center
assina `programs: []` para todos os membros. Interpretar essa lista como concessão
obrigatória apenas no Mind bloquearia todas as consultas e contribuições após o
deploy. A implementação da identidade, das concessões e do portal pertence ao
projeto irmão `../api-hive-center` / `../hive_center`.

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
- Expiração de token e revogação de membro/programa têm prazo de corte definido e
  testado no Center. O filtro de classificação e a aprovação de escopo continuam
  controles distintos da autorização do membro.

## Aceite cruzado antes de ativar

Com dois membros e dois programas sintéticos, cada membro consegue ler e
contribuir apenas no programa concedido. Busca direta, contexto, catálogo global,
ingestão e aprovação tentados no outro programa retornam autorização negada sem
divulgar conteúdo ou existência do programa. Remover a concessão impede novos
pedidos dentro do prazo de revogação publicado. O rollout atual do Center deve
ser atualizado antes de habilitar essa regra no Mind; nenhuma alteração neste
checkout deve tratar `programs: []` como autorização global definitiva.
