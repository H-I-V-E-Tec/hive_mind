# Base Vetorial de Recon para Segurança Ofensiva Autorizada

## Objetivo

Evoluir este projeto para servir como uma base de conhecimento vetorial privada, compartilhada por uma organização, para investigações de segurança autorizadas, programas de bug bounty e atividades de red team dentro de escopo.

A plataforma deve reduzir a necessidade de reenviar logs, regras e anotações extensas aos agentes de IA. Em vez disso, Claude, Codex e outros clientes consultam apenas os trechos relevantes, autorizados e atualizados para cada engajamento.

## Fontes de conhecimento

- Escopos, regras de engajamento e exclusões de programas.
- Resultados normalizados de recon: ativos, subdomínios, IPs, portas, tecnologias, endpoints e parâmetros.
- Evidências, anotações, hipóteses e decisões de investigação.
- Metodologias, playbooks, checklists e documentação interna.
- Write-ups e relatórios públicos aprovados para consulta.
- Histórico de validações e hipóteses descartadas, evitando trabalho repetido.

## Fluxo proposto

```text
Fontes de recon e conhecimento
  └─ Markdown / JSON / logs normalizados / evidências
       └─ Ingestor: valida, normaliza e gera embeddings
            └─ Qdrant privado
                 └─ API/MCP central com autenticação e autorização
                      └─ Agentes de IA dos membros da organização
```

## Ganhos esperados

- Menor consumo de tokens: recuperação de poucos trechos relevantes em vez de logs e documentos completos.
- Maior assertividade: o agente cruza ativos, tecnologias, endpoints, evidências e regras do programa.
- Continuidade entre sessões e entre membros autorizados do mesmo engajamento.
- Menos duplicação: pesquisas revelam verificações, utilitários e hipóteses anteriores.
- Privacidade e controle: Qdrant e o gerador de embeddings podem permanecer em infraestrutura privada.

## Metadados mínimos por documento

Cada item indexado deve carregar metadados estruturados para permitir filtros e isolamento:

```json
{
  "organization_id": "org-123",
  "program_id": "program-456",
  "engagement_id": "eng-789",
  "asset": "api.example.com",
  "source": "httpx",
  "collected_at": "2026-09-09T12:00:00Z",
  "classification": "internal",
  "scope_status": "in_scope",
  "tags": ["recon", "http", "oauth"]
}
```

## Requisitos para operação compartilhada

O servidor MCP atual foi concebido principalmente para uso local via `stdio`, observando um diretório. Para uma organização, a exposição deve ser feita por um serviço central, e não pela publicação direta desse processo na internet.

- Autenticação forte e autorização por organização, time, programa e engajamento.
- Isolamento obrigatório por tenant/projeto e filtros de permissão aplicados no servidor, nunca confiados ao cliente.
- Filtros por escopo e status do ativo antes de devolver contexto ao agente.
- Auditoria de ingestões e consultas: usuário, momento, fonte e documentos retornados.
- Criptografia em trânsito e em repouso, além de gestão apropriada de segredos.
- Política de retenção, classificação e remoção de evidências sensíveis.
- Rate limits, limites de resultados e proteção contra vazamento indevido de contexto.
- Pipeline de ingestão controlado via API/fila, em vez de o serviço central observar diretamente máquinas de membros.

## Evolução sugerida deste projeto

Separar a solução em dois componentes:

1. **Ingestor**: recebe documentos e resultados de recon, valida escopo e formato, normaliza conteúdo, anexa metadados e grava vetores no Qdrant.
2. **API/MCP central**: autentica o membro, resolve suas permissões e pesquisa exclusivamente os documentos permitidos para aquele programa ou engajamento.

O mecanismo atual de indexação e busca semântica é uma boa base. Para esse domínio, as prioridades são ingestão de documentos/JSON, filtros de metadados no Qdrant, busca híbrida quando útil e governança de acesso.

## Limites de uso

A plataforma deve ser usada somente para ativos explicitamente autorizados. O RAG auxilia a organizar e analisar contexto; ele não amplia escopo, não substitui validação humana e não elimina a obrigação de seguir as regras do programa.

## Visão de cooperativa de segurança

A proposta pode evoluir de uma ferramenta de busca para uma cooperativa que fornece estrutura e infraestrutura a caçadores de vulnerabilidades autorizados. O objetivo não é apenas disponibilizar ferramentas, mas aumentar a capacidade coletiva de cada membro sem retirar sua autonomia.

Os pilares iniciais são:

| Pilar | Produto prático | Valor ao membro |
| --- | --- | --- |
| Conhecimento e mentoria | Playbooks, RAG, revisão e memória coletiva | Aprende e decide mais rápido |
| Ferramentas e infraestrutura | Qdrant privado, ingestão, agentes, ambientes e automação | Reduz custo e fricção operacional |
| Conexão | Times, colaboração por programa e reputação interna | Acesso a oportunidades e especialistas |
| Mente coletiva | Recon e aprendizados compartilhados com permissões | Menos retrabalho e mais cobertura |
| Multiagente | Handoff entre modelos e estado persistente | Menos interrupção e dependência de um único fornecedor |

## Mente coletiva autorizada

A base vetorial criptografada pode centralizar, com controles de acesso, escopos, evidências, endpoints, tecnologias, hipóteses, anotações e aprendizados. Cada agente consulta somente o contexto permitido para seu programa e engajamento.

Benefícios esperados:

- Evitar duplicação: antes de investigar ou reportar, o membro e seu agente encontram testes, hipóteses e achados já registrados.
- Criar recon acumulativo: um subdomínio, endpoint ou fluxo já descoberto acelera investigações autorizadas futuras.
- Preservar conhecimento: aprendizados não desaparecem quando um membro deixa uma investigação ou a organização.
- Escalar mentoria: playbooks e decisões de membros experientes tornam-se recuperáveis no momento em que são necessários.
- Melhorar triagem: consultas podem relacionar ativos, tecnologias, endpoints, evidências e requisitos de escopo.
- Reduzir custo de IA: os agentes recebem contexto relevante em vez de logs e documentos inteiros.

## Continuidade multiagente

O produto pode permitir que membros escolham Claude, Codex ou outros modelos conforme especialidade, custo e disponibilidade de tokens.

Não existe transferência automática de uma sessão idêntica entre provedores: cada modelo possui histórico, ferramentas e comportamento próprios. A continuidade deve ser construída por meio de um **estado canônico de investigação**, persistido e controlado pela plataforma.

```text
Sessão de investigação
  ├─ objetivo, escopo e regras
  ├─ ativos e evidências relevantes
  ├─ hipóteses e testes já executados
  ├─ conclusões e próximos passos
  └─ referências aos documentos vetorizados
         ↓
Claude atinge o limite de tokens
         ↓
Plataforma gera handoff estruturado
         ↓
Codex recebe estado + contexto recuperado
         ↓
Investigação continua
```

O handoff deve conter um resumo estruturado, evidências, decisões tomadas, pendências, limites de escopo e referências aos documentos autorizados. Isso evita depender de uma conversa crua e permite retomar o trabalho com outro agente de forma previsível.

## Confiança, atribuição e governança

Para a mente coletiva gerar valor sem criar riscos, a plataforma precisa tratar confiança como requisito de produto:

- Compartilhamento explícito e granular: o autor define o que pode ser compartilhado, com qual time e em qual programa.
- Isolamento de informações sensíveis por programa, organização e engajamento.
- Registro de autoria, contribuição e uso de cada evidência ou descoberta, para suportar atribuição e possíveis regras de recompensa.
- Auditoria de acessos, consultas, ingestões e handoffs entre agentes.
- Criptografia, política de retenção e exclusão de dados, além de gestão segura de credenciais.
- Revisão humana para conteúdo compartilhado, achados críticos e alterações de escopo.

Sem esses controles, uma base coletiva pode vazar informações sensíveis, gerar disputas por crédito ou violar regras de programas. A plataforma deve sempre operar dentro do escopo explicitamente autorizado.
