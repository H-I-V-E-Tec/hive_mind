# Hive Mind 2.0.0 — plano de melhorias

Data: 2026-10-13. Estado: execução iniciada. Este plano organiza mudanças no cliente e no serviço Mind deste repositório. A identidade, as concessões de programas, o portal e a persistência canônica do produto continuam nos projetos do Center. Os contratos compartilhados e os testes do cliente ficam aqui.

## Objetivo e ordem de entrega

Entregar um Mind que sincronize arquivos sem perdas silenciosas, aplique a autorização do membro por programa, controle custo e tamanho de requisições, recupere evidências relevantes e tenha instalação e aceite verificáveis. Cada etapa abaixo deve ter mudança pequena, teste sintético local e evidência do serviço real isolado quando o critério depender de Qdrant, Ollama ou Center.

| Ordem | Entrega no `hive_mind` | Critério de aceite | Estado |
| --- | --- | --- | --- |
| 1 | Corrigir `Rename`, cancelamento e ciclo de vida do debounce do watcher. Depois, reconciliar movimentações de diretórios com inventário seguro. | Renomear um arquivo marca a origem como ausente; eventos não bloqueiam o desligamento; timers pendentes são cancelados. Movimento de diretório passa em teste com Qdrant isolado antes de ser considerado concluído. | Parcial: correção de arquivo e cancelamento nesta primeira mudança. |
| 2 | Integrar o contrato de [autorização por programa](docs/spec/contracts/program-access.md) em busca, contexto, catálogo, ingestão e aprovação de escopo. | Dois membros e dois programas sintéticos: cada membro acessa apenas seu programa; catálogo global não revela programas alheios; revogação corta o acesso no prazo definido pelo Center. | Pendente; exige contrato versionado e concessões efetivas no Center antes de ativar a regra. |
| 3 | Limitar corpos e concorrência das rotas HTTP; reduzir buscas repetidas de chaves JWKS inexistentes; separar saúde do processo de prontidão das dependências. | Pedidos grandes recebem `413`, sobrecarga recebe resposta controlada, tokens inválidos não amplificam chamadas ao Center e a prontidão falha se dependências obrigatórias estiverem indisponíveis. | Implementado no código e em testes isolados; ensaio de carga e aceite com dependências reais ainda pendentes. |
| 4 | Expor modos de busca avaliados e aumentar diversidade por documento, respeitando o orçamento de resposta. | Comparação denso/híbrido no corpus sintético e em Qdrant/Ollama isolados; registrar recall, MRR, documentos distintos por top-k, latência e bytes. Não mudar o padrão sem medição. | Pendente. |
| 5 | Tornar o teste de ingestão com Qdrant e Ollama descartáveis obrigatório no caminho de release e medir limites de catálogo/status em corpus crescente. | CI executa ingestão → replay → busca com serviços reais; status e catálogo paginam ou informam cobertura parcial sem perda silenciosa; specs operacionais só mudam de estado com evidência do ambiente exigido. | Pendente; teste real opt-in já existe. |
| 6 | Consolidar instalação verificada e documentação pública. | Instalador direto exige assinatura verificável ou aponta para o launcher verificado; teste de instalação por SO anunciado; links e descrição de checksum correspondem ao fluxo atual. | Parcial: descrição do checksum corrigida; assinatura obrigatória e teste de instalação pendentes. |

## Fronteiras e migração

- O Center deve emitir uma versão explícita do contrato de programas e concessões de leitura/contribuição. O Mind só passa a exigir `programs` após teste de compatibilidade e plano de rollout; `programs: []` atualmente é emitido para todos, conforme o contrato.
- A persistência canônica, outbox, curadoria e revogação de membro pertencem ao serviço comercial definido no [plano de melhoria e limpeza](PLANO_MELHORIA_E_LIMPEZA.md) e no projeto irmão. Mudanças de cliente neste repositório precisam preservar os formatos atuais até haver migração testada.
- Os testes usam fixtures e diretórios temporários, nunca o acervo ou as collections de produção. Aceite de infraestrutura exige Qdrant/Ollama descartáveis e, para autorização, Center de teste com membros e programas sintéticos.
- Publicar 2.0.0 somente depois de verificar atualização a partir da versão instalada anterior, compatibilidade cliente/serviço, restauração e rollback, além dos critérios acima. Registrar evidências sem segredos e sem dados de clientes.

## Mudanças iniciadas

`server/watcher.go`: encaminhar `Rename` do arquivo antigo para a lógica de revisão, permitir cancelamento com fila cheia, remover timers de debounce após disparo e limpar os pendentes no desligamento. Os testes ficam em `server/watcher_test.go`. Essa entrega cobre arquivos individuais; movimentos de diretório exigem reconciliação própria e teste com serviços reais antes do aceite da etapa 1 completa.

`server/http.go`: limitar a 64 KiB o corpo das rotas de busca, contexto e catálogo, com `413` para excesso e `400` para JSON inválido ou concatenado. A ingestão conserva seu limite específico de 6 MiB. Os testes HTTP verificam as três rotas e o rejeite de JSON concatenado.

O adaptador limita a 32 requisições autenticadas simultâneas e devolve `503` com `Retry-After` na saturação. `GET /healthz` confirma que o processo responde; `GET /readyz` verifica as duas collections, o modelo de embedding no Ollama e as chaves públicas do Center. A sondagem usa uma janela de 15 segundos para o Center e um prazo total de 5 segundos. O deploy passa a exigir `/readyz` antes de aceitar a nova versão; os testes de rollback cobrem sua falha.

O cliente JWKS serializa consultas concorrentes, limita a resposta a 1 MiB e reduz novas consultas para `kid` inexistente durante 10 segundos, ou para falha do Center durante 5 segundos. Uma chave recém-rotacionada pode levar até 10 segundos para ser reconhecida após uma consulta negativa; esse intervalo é intencional e precisa ser compatível com a janela de rotação do Center.
