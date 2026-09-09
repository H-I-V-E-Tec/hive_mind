# Hive Mind v0.1 — MCP de Recon Compartilhado

## Objetivo da primeira versão

Disponibilizar uma memória coletiva simples para **dois computadores autorizados**, usando este projeto como base. O resultado deve ser um servidor MCP capaz de indexar e pesquisar notas e dados de recon com busca semântica, retornando contexto curto e relevante para reduzir o consumo de tokens de agentes como Claude e Codex.

Esta versão não tenta resolver colaboração comercial, permissões granulares, cobrança, interface web ou troca automática de modelos. Ela deve provar o fluxo essencial: **registrar conhecimento em um computador e recuperá-lo, com segurança, no outro**.

## Escopo v0.1

- Uma instância privada de Qdrant, acessível apenas pelos dois computadores ou por uma rede privada/VPN.
- Um servidor MCP por computador, configurado para usar a mesma collection no Qdrant.
- Ollama local em cada computador, com o mesmo modelo de embeddings e a mesma dimensão vetorial.
- Ingestão de arquivos de recon normalizados em uma pasta compartilhada ou sincronizada.
- Busca semântica por dados de recon, anotações, escopo e evidências.
- Filtros básicos por tipo de arquivo e por diretório/campanha.
- Convenções simples de organização de arquivos e operação manual.

## Fora do escopo v0.1

- Exposição pública do MCP pela internet.
- Acesso de múltiplas organizações, RBAC, cobrança ou painel web.
- Handoff automático entre Claude, Codex e outros modelos.
- Execução remota de scanners ou automações ofensivas.
- Compartilhamento de dados fora de ativos e programas explicitamente autorizados.

## Arquitetura mínima

```text
Computador A                                      Computador B
─────────────                                      ─────────────
Arquivos de recon ─┐                             ┌─ Arquivos de recon
Ollama local      │                             │  Ollama local
MCP local         │                             │  MCP local
                  └──── rede privada / VPN ─────┘
                              │
                    Qdrant privado central
                    collection: hive_mind_v01
```

O MCP continua local e é conectado ao Claude/Codex via `stdio`. Somente o Qdrant é compartilhado. Isso evita expor o protocolo MCP diretamente na rede e mantém as credenciais e as configurações dos agentes em cada máquina.

## Decisão operacional importante

Os dois computadores devem usar o **mesmo `EMBEDDING_MODEL`**. Um modelo de embedding diferente pode gerar vetores incompatíveis com a collection existente e degradar ou invalidar a busca.

Para a v0.1, recomenda-se usar `nomic-embed-text` em ambos e registrar esse padrão em um arquivo de configuração compartilhado.

## Organização sugerida dos dados

Usar texto e Markdown como formato inicial, pois este projeto já indexa documentos. Manter arquivos pequenos, específicos e legíveis facilita a busca e auditoria.

```text
hive-data/
  README.md
  programs/
    acme-bugbounty/
      scope.md
      rules.md
      recon/
        assets.md
        subdomains.md
        endpoints.md
        technologies.md
      notes/
        2026-09-09-auth-flow.md
        hypotheses.md
      evidence/
        endpoint-api-example-com.md
```

Cada documento deve começar com um cabeçalho simples, por exemplo:

```markdown
# API: api.example.com

- Programa: acme-bugbounty
- Escopo: autorizado
- Classificação: interno
- Fonte: httpx em 2026-09-09
- Tags: recon, http, oauth

## Observações

O host expõe autenticação OAuth e endpoint de upload em `/v1/files`.
```

## Alterações necessárias neste projeto

### 1. Tornar a configuração adequada ao Hive Mind

- Adicionar `HIVE_MODE=true` para ativar comportamento voltado a documentos de recon.
- Adicionar `HIVE_ID`, `HIVE_DATA_DIR` e `HIVE_COLLECTION` (ou reutilizar `WATCH_DIRECTORY` e `QDRANT_COLLECTION` com nomes bem documentados).
- Alterar o padrão de `PARSER_MODE` para `doc` quando `HIVE_MODE=true`.
- Criar um arquivo de exemplo, como `.env.hive.example`, sem credenciais reais.

### 2. Melhorar a ingestão de documentos

- Priorizar `.md`, `.txt` e `.json` no modo Hive.
- Manter o conteúdo JSON legível para indexação; numa etapa posterior, extrair campos estruturados.
- Ajustar o tamanho e a sobreposição dos chunks para notas e saídas de recon, evitando separar uma evidência de seu cabeçalho.
- Preservar caminho relativo, data de indexação e hash de conteúdo em cada ponto do Qdrant.
- Continuar respeitando `.gitignore` e limites de tamanho de arquivo.

### 3. Adicionar metadados mínimos no Qdrant

Extrair, inicialmente do cabeçalho Markdown ou do caminho, e persistir estes campos:

- `hive_id`
- `program_id`
- `document_type` (`scope`, `rules`, `asset`, `endpoint`, `note`, `evidence`)
- `scope_status` (`authorized`, `out_of_scope`, `unknown`)
- `classification` (`internal`, `restricted`)
- `source`
- `collected_at`
- `tags`
- `path`

Na v0.1, se a extração automática não estiver pronta, esses valores podem permanecer como texto no documento. O mínimo inegociável é indexar o `path` e o `hive_id` em todo ponto.

### 4. Evoluir a ferramenta MCP de busca

Expandir `qdrant_search` com filtros opcionais, aplicados no servidor:

```json
{
  "query": "hosts autorizados com OAuth e upload",
  "program_id": "acme-bugbounty",
  "document_types": ["asset", "endpoint", "scope"],
  "tags": ["oauth"],
  "limit": 8
}
```

Também adicionar uma ferramenta inicial, por exemplo `hive_get_context`, que receba o programa e uma pergunta e devolva uma resposta compacta contendo:

- regras e limites de escopo relevantes;
- até alguns trechos de recon/evidência relacionados;
- caminhos e fontes dos documentos;
- aviso explícito quando não houver confirmação de escopo.

O retorno deve ter limite de tamanho configurável para proteger o orçamento de tokens.

### 5. Adicionar proteção básica de acesso

- Nunca deixar Qdrant exposto publicamente; permitir apenas LAN confiável ou VPN privada (por exemplo, Tailscale/WireGuard).
- Usar TLS se houver tráfego fora da mesma máquina/rede privada.
- Configurar uma API key do Qdrant e guardá-la somente em variáveis de ambiente/gerenciador de segredos; nunca no repositório.
- Usar uma collection exclusiva, por exemplo `hive_mind_v01`, sem misturá-la à coleção de código deste projeto.
- Registrar logs de indexação e consultas localmente, sem registrar conteúdo sensível desnecessário.

### 6. Criar comandos operacionais simples

- `ingest`: indexa toda a pasta `hive-data`.
- `search <consulta>`: permite validar uma busca no terminal.
- `status`: mostra collection, modelo, arquivos sincronizados e pendências.
- `validate`: verifica conectividade com Qdrant e Ollama e confirma a compatibilidade do modelo/dimensão.

## O que pode ser feito hoje, sem alterar o código

1. Subir um Qdrant privado em um dos computadores ou em um servidor próprio, protegido por VPN.
2. Instalar o mesmo Ollama e o mesmo modelo de embeddings nos dois computadores.
3. Criar e sincronizar a pasta `hive-data` via Git privado, Syncthing ou armazenamento privado. Não incluir segredos, tokens ou evidências que não devam ser replicadas.
4. Definir uma collection única: `hive_mind_v01`.
5. Configurar ambos os MCPs com o mesmo Qdrant e a mesma pasta de dados sincronizada.
6. Usar `PARSER_MODE=doc`, `SEARCH_MODE=hybrid` (ou `dense` inicialmente) e `INCLUDE_HIDDEN_DIRS` apenas se necessário.
7. Rodar `qdrant-mcp-server ingest` no primeiro computador, verificar resultados, e então testar consultas no segundo.

## Configuração de referência

```bash
QDRANT_HOST=10.0.0.10
QDRANT_PORT=6334
QDRANT_COLLECTION=hive_mind_v01
WATCH_DIRECTORY=/caminho/para/hive-data
OLLAMA_HOST=http://127.0.0.1:11434
EMBEDDING_MODEL=nomic-embed-text
PARSER_MODE=doc
SEARCH_MODE=dense
EXCLUDE_DIRS=.git,node_modules
```

## Critérios de sucesso

- Uma nota criada no Computador A é sincronizada e indexada na collection compartilhada.
- Uma busca feita pelo MCP no Computador B encontra essa nota com caminho e contexto suficientes para verificação.
- Perguntas de recon retornam menos contexto do que carregar os arquivos inteiros, sem perder as informações essenciais.
- Consultas relacionadas a ativos sem confirmação de escopo retornam aviso ou não são tratadas como autorizadas.
- O Qdrant não está acessível pela internet pública e credenciais não estão versionadas.

## Próximas fases, após validar a v0.1

1. Extração robusta de metadados de Markdown e JSON, com filtros nativos no Qdrant.
2. API central para ingestão e busca, com autenticação de usuários.
3. Controle de acesso por organização, programa, time e engajamento.
4. Auditoria, autoria, atribuição de contribuição e políticas de compartilhamento.
5. Estado canônico de investigação e handoff estruturado entre agentes/modelos.
6. Interface para visualizar ativos, evidências, relações e histórico de investigações.
