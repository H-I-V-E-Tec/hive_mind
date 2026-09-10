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
- metadados de recon normalizados e escopo efetivo calculado somente de manifestos aprovados;
- aprovação local de `scope.json`, invalidação por hash e rematerialização sem recalcular embeddings;
- busca densa, esparsa ou híbrida com filtros nativos de Hive, programa, escopo, classificação, tipo e tags;
- ferramentas MCP estruturadas `hive_search` e `hive_get_context`, além de `get_sync_status` e `ingest_workspace`;
- comandos `ingest [--prune]`, `remove`, `scope approve <program_id>` e `search <program_id> <query>`.

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
HIVE_CONTEXT_MAX_CHARS=12000
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

As specs 00 a 05 estão implementadas na rota Hive: papéis, topologia de controle, configuração segura, ingestão revisionada, metadados normalizados, autorização de escopo baseada em manifesto aprovado, busca MCP filtrada e contexto compacto por ativo. Implantação privada, operação completa e aceite ponta a ponta continuam nas specs seguintes; portanto, o produto completo ainda não satisfaz todas as specs Hive.

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

Exemplo de nota não autoritativa (o front matter é extraído para cada chunk):

```markdown
---
program_id: acme-bugbounty
document_type: note
claimed_scope_status: authorized
classification: internal
source: httpx
collected_at: 2026-09-09T12:00:00Z
tags: [recon, http, oauth]
asset_refs: [api.example.com]
---
# API: api.example.com

## Observações

O host expõe autenticação OAuth e endpoint de upload em `/v1/files`.
```

`claimed_scope_status` nunca concede autorização. Para ativar um manifesto válido em `programs/<program_id>/scope.json`, execute no writer:

```bash
hive-mind scope approve acme-bugbounty
```

O comando valida e normaliza as regras, rematerializa os chunks existentes sem recalcular embeddings e só então publica o hash exato do manifesto. Alterar ou remover `scope.json` invalida a aprovação e faz o programa falhar fechado como `unknown` até uma nova aprovação.

## Busca MCP

`hive_search` exige `query` e `program_id`. Também aceita `document_types`, `tags` (semântica `ALL`), `classification`, `effective_scope_status` e `limit` de 1 a 20, com padrão 8. Hive, collection, revisão e elevação de classificação são sempre definidos pelo servidor e não podem ser enviados pelo cliente.

```json
{
  "query": "hosts autorizados com OAuth",
  "program_id": "acme-bugbounty",
  "document_types": ["asset", "endpoint"],
  "tags": ["oauth"],
  "effective_scope_status": "authorized",
  "limit": 8
}
```

A resposta estruturada contém `results`, `warnings` e `truncated`. Cada resultado inclui texto, score, path relativo, proveniência, tipo, classificação, escopo efetivo e `untrusted_content: true`. O servidor filtra a revisão de escopo no Qdrant e confirma novamente isolamento, classificação e revisão ativa antes de retornar qualquer trecho.

`hive_get_context` recebe uma pergunta e um ativo explícito, sem tentar inferir autorização do texto:

```json
{
  "program_id": "acme-bugbounty",
  "question": "o que sabemos sobre este host?",
  "asset": {"type": "host", "value": "api.example.com"},
  "limit": 8
}
```

O pacote resultante apresenta primeiro a decisão do manifesto aprovado e as regras aplicáveis; documentos de regras e evidências não confiáveis vêm depois. Em estado `out_of_scope`, notas e endpoints acionáveis não são incluídos. `HIVE_CONTEXT_MAX_CHARS`, com padrão 12.000 e faixa 1.000–50.000 caracteres Unicode, limita toda a resposta serializada removendo itens inteiros menos relevantes.

## Segurança e uso autorizado

Use Hive Mind somente para ativos e programas explicitamente autorizados. Não exponha Qdrant ou MCP publicamente. Restrinja o Qdrant a uma LAN confiável ou VPN, habilite TLS quando houver tráfego fora da máquina/rede privada e mantenha chaves fora do repositório.

## Planejamento

As especificações em [docs/spec/](docs/spec/README.md) dividem a implementação em etapas verificáveis, incluindo um conjunto próprio de [specs de segurança](docs/spec/security/README.md). As [decisões de projeto](docs/decisions/README.md) registram as regras que orientam todas as etapas. Esses documentos devem ser atualizados na mesma alteração que modificar o comportamento do produto.

## Desenvolvimento

```bash
go test ./...
go build ./...
```
