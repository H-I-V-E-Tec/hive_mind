# Hive Mind

Hive Mind é um servidor [Model Context Protocol (MCP)](https://modelcontextprotocol.io) escrito em Go para criar uma memória de recon compartilhada entre computadores autorizados. Ele indexa documentos locais com embeddings do Ollama, armazena os vetores em uma collection privada do Qdrant e oferece busca semântica aos agentes por `stdio`.

O primeiro objetivo é simples: uma nota ou evidência de recon registrada em um computador deve poder ser recuperada com contexto curto e verificável no outro, sem expor o servidor MCP à internet.

> Este repositório será refeito como um produto Hive Mind dedicado. O código atual é somente uma base de referência e não define requisitos de compatibilidade. As funcionalidades alvo estão em [docs/spec/](docs/spec/README.md); as regras arquiteturais estão em [docs/decisions/](docs/decisions/README.md).

## Como funciona

```text
Computador A                                  Computador B
────────────                                  ────────────
hive-data/ ── MCP local ── Ollama local       hive-data/ ── MCP local ── Ollama local
                  │                                      │
                  └──────── rede privada/VPN ────────────┘
                                      │
                         Qdrant privado compartilhado
                         collection: hive_mind_v01
```

Os arquivos de `hive-data/` são sincronizados por um meio privado escolhido pela equipe, como Git privado ou Syncthing. Em cada máquina, o processo local observa e indexa os arquivos. Os dois processos usam a mesma collection e o mesmo modelo de embeddings. O agente conecta-se somente ao MCP local via `stdio`; o componente compartilhado é o Qdrant, protegido por LAN confiável ou VPN.

## Estado atual

O código já oferece:

- indexação recursiva e observação de alterações no diretório configurado;
- embeddings locais via Ollama e armazenamento vetorial no Qdrant por gRPC;
- parsing de código e documentos, chunking, hashes de conteúdo e respeito a `.gitignore`;
- busca densa, esparsa ou híbrida por `qdrant_search`, com filtros por extensão e caminho;
- ferramentas MCP `qdrant_search`, `get_sync_status` e `ingest_workspace`;
- comandos `ingest`, `search` e `evaluate-search`.

A implementação será substituída gradualmente pelos componentes descritos nas [specs](docs/spec/README.md). Não há requisito de preservar variáveis, comandos, formatos de payload ou ferramentas MCP do servidor RAG anterior.

## Pré-requisitos

- Go 1.25 ou superior;
- um Qdrant privado acessível pelas máquinas autorizadas;
- Ollama em cada máquina, com o mesmo modelo de embeddings instalado;
- uma pasta de dados compartilhada contendo Markdown, texto ou JSON de recon.

Todos os computadores devem usar o mesmo `EMBEDDING_MODEL`. Vetores gerados por modelos ou dimensões diferentes são incompatíveis com a mesma collection.

## Configuração inicial

Use variáveis de ambiente ou a configuração MCP do seu cliente. Esta é uma configuração de referência para o alvo v0.1:

```bash
QDRANT_HOST=10.0.0.10
QDRANT_PORT=6334
HIVE_ID=research-team
HIVE_COLLECTION=hive_mind_v01
HIVE_DATA_DIR=/caminho/para/hive-data
OLLAMA_HOST=http://127.0.0.1:11434
EMBEDDING_MODEL=nomic-embed-text
PARSER_MODE=doc
SEARCH_MODE=dense
EXCLUDE_DIRS=.git,node_modules
```

`HIVE_ID`, `HIVE_COLLECTION`, `HIVE_DATA_DIR`, `OLLAMA_HOST` e `EMBEDDING_MODEL` serão obrigatórias no produto Hive Mind. O contrato completo está na [spec de configuração](docs/spec/01-hive-configuration.md).

Compile e faça a primeira ingestão:

```bash
go build -o hive-mind .
./hive-mind ingest
./hive-mind search "hosts autorizados com OAuth e upload"
```

Sem argumentos, o binário inicia o servidor MCP e a observação do diretório. O MCP também permite iniciar uma ingestão por `ingest_workspace` e consultar o andamento por `get_sync_status`.

## Formato inicial dos documentos

Prefira documentos pequenos, específicos e legíveis. A estrutura de referência é:

```text
hive-data/
  programs/
    acme-bugbounty/
      scope.md
      rules.md
      recon/
        assets.md
        endpoints.md
      notes/
        2026-09-09-auth-flow.md
      evidence/
        endpoint-api-example-com.md
```

Exemplo de nota:

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

Na versão atual, essas informações são pesquisáveis por estarem no texto. As próximas etapas vão extraí-las para metadados filtráveis.

## Segurança e uso autorizado

Use Hive Mind somente para ativos e programas explicitamente autorizados. Não exponha Qdrant ou MCP publicamente. Restrinja o Qdrant a uma LAN confiável ou VPN, habilite TLS quando houver tráfego fora da máquina/rede privada e mantenha chaves fora do repositório.

## Planejamento

As especificações em [docs/spec/](docs/spec/README.md) dividem a implementação em etapas verificáveis, incluindo um conjunto próprio de [specs de segurança](docs/spec/security/README.md). As [decisões de projeto](docs/decisions/README.md) registram as regras que orientam todas as etapas. Esses documentos devem ser atualizados na mesma alteração que modificar o comportamento do produto.

## Desenvolvimento

```bash
go test ./...
go build ./...
```
