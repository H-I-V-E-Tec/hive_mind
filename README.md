# Hive Mind

Hive Mind é um servidor [Model Context Protocol (MCP)](https://modelcontextprotocol.io) escrito em Go para criar uma memória de recon compartilhada entre computadores autorizados. Ele indexa documentos locais com embeddings do Ollama, armazena os vetores em uma collection privada do Qdrant e oferece busca semântica aos agentes por `stdio`.

O primeiro objetivo é simples: uma nota ou evidência de recon registrada em um computador deve poder ser recuperada com contexto curto e verificável no outro, sem expor o servidor MCP à internet.

> Este repositório será refeito como um produto Hive Mind dedicado. O código atual é somente uma base de referência e não define requisitos de compatibilidade. As funcionalidades alvo estão em [docs/spec/](docs/spec/README.md); as regras arquiteturais estão em [docs/decisions/](docs/decisions/README.md).

## Como funciona

```text
Computador A (writer)                         Computador B (reader)
─────────────────────                         ─────────────────────
hive-data canônico ─ MCP local                MCP local ─ Ollama local
       │                │                         │
  Ollama local          └────── TLS/VPN ─────────┤
                                                 │
                                    Qdrant privado compartilhado
                                    dados + controle de revisões
```

Os arquivos de `hive-data/` podem ser sincronizados por um meio privado, como Git privado ou Syncthing, mas somente o writer observa a cópia canônica e altera o Qdrant. O reader apenas pesquisa. Ambos usam o mesmo fingerprint de embeddings. O agente conecta-se somente ao MCP local via `stdio`; o componente compartilhado é o Qdrant, protegido por TLS sobre LAN/VPN e por credenciais distintas de menor privilégio.

## Estado atual

O código já oferece:

- indexação recursiva e observação de alterações no diretório configurado;
- embeddings locais via Ollama e armazenamento vetorial no Qdrant por gRPC;
- ingestão restrita a Markdown, texto e JSON, com chunking limitado, hashes e respeito a `.gitignore`;
- publicação revisionada: staging confirmado, verificação, `document_head` e limpeza posterior;
- collections separadas de dados e controle, manifesto imutável e registro de writer único;
- busca densa, esparsa ou híbrida isolada por Hive e programa, descartando revisões inativas;
- ferramentas MCP `qdrant_search`, `get_sync_status` e `ingest_workspace`;
- comandos `ingest [--prune]`, `remove` e `search <program_id> <query>`.

A implementação será substituída gradualmente pelos componentes descritos nas [specs](docs/spec/README.md). Não há requisito de preservar variáveis, comandos, formatos de payload ou ferramentas MCP do servidor RAG anterior.

## Pré-requisitos

- Go 1.25 ou superior;
- um Qdrant privado acessível pelas máquinas autorizadas;
- Ollama em cada máquina, com o mesmo modelo de embeddings instalado;
- uma pasta de dados compartilhada contendo Markdown, texto ou JSON de recon.

Todos os computadores devem usar o mesmo `EMBEDDING_MODEL`. Vetores gerados por modelos ou dimensões diferentes são incompatíveis com a mesma collection.

## Configuração Hive

O contrato da spec 01 já está implementado. A configuração falha antes de criar clientes ou watchers quando estiver ausente, inválida ou insegura; variáveis legadas e autodescoberta não são aceitas.

Exemplo do writer:

```bash
QDRANT_URL=https://qdrant.hive.internal:6334
QDRANT_API_KEY=replace-via-secret-manager
QDRANT_TLS_CA_FILE=/caminho/para/hive-ca.pem
HIVE_ID=research-team
HIVE_DEVICE_ID=workstation-a
HIVE_ROLE=writer
HIVE_WRITER_APPROVAL_ID=change-1042
HIVE_COLLECTION=hive_mind_v01
HIVE_DATA_DIR=/caminho/para/hive-data
OLLAMA_URL=http://127.0.0.1:11434
EMBEDDING_MODEL=nomic-embed-text
```

O reader usa `HIVE_ROLE=reader`, outro `HIVE_DEVICE_ID`, credencial read-only própria e não precisa de `HIVE_DATA_DIR`. O contrato completo está na [spec de configuração](docs/spec/01-hive-configuration.md) e as invariantes estão na [spec 00](docs/spec/00-system-invariants.md).

## Compilação e testes

Compile o binário e execute a suíte:

```bash
go build -o hive-mind .
go test ./...
```

As specs 00, 01 e 02 estão implementadas na rota Hive: papéis, topologia de controle, configuração segura e ingestão revisionada. Metadados completos, aprovação de escopo, API final de busca, operação segura e aceite ponta a ponta continuam nas specs seguintes; portanto, o produto completo ainda não satisfaz todas as specs Hive.

## Formato inicial dos documentos

Prefira documentos pequenos, específicos e legíveis. A estrutura de referência é:

```text
hive-data/
  programs/
    acme-bugbounty/
      scope.json
      rules.md
      recon/
        assets.md
        endpoints.md
      notes/
        2026-09-09-auth-flow.md
      evidence/
        endpoint-api-example-com.md
```

Exemplo de nota não autoritativa:

```markdown
# API: api.example.com

- Programa: acme-bugbounty
- Escopo declarado: autorizado
- Classificação: interno
- Fonte: httpx em 2026-09-09
- Tags: recon, http, oauth

## Observações

O host expõe autenticação OAuth e endpoint de upload em `/v1/files`.
```

Na versão atual, essas informações são pesquisáveis por estarem no texto, mas a declaração da nota permanece `unknown` e nunca concede autorização. A extração completa dos metadados e o fluxo de aprovação de `scope.json` pertencem às specs 03 e 07.

## Segurança e uso autorizado

Use Hive Mind somente para ativos e programas explicitamente autorizados. Não exponha Qdrant ou MCP publicamente. Restrinja o Qdrant a uma LAN confiável ou VPN, habilite TLS quando houver tráfego fora da máquina/rede privada e mantenha chaves fora do repositório.

## Planejamento

As especificações em [docs/spec/](docs/spec/README.md) dividem a implementação em etapas verificáveis, incluindo um conjunto próprio de [specs de segurança](docs/spec/security/README.md). As [decisões de projeto](docs/decisions/README.md) registram as regras que orientam todas as etapas. Esses documentos devem ser atualizados na mesma alteração que modificar o comportamento do produto.

## Desenvolvimento

```bash
go test ./...
go build ./...
```
