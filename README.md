# Hive Mind

Hive Mind é um núcleo de memória privada para trabalho de segurança autorizado. Este repositório contém o código Go, contratos, testes e automações de release. O cliente operacional é o binário `hive` publicado nas releases; agentes de trabalho não usam este checkout como fonte de documentos ou servidor MCP.

## O que funciona hoje

O executável inicia um MCP local via `stdio`. Um writer observa uma pasta de documentos e publica revisões no Qdrant privado; readers consultam o mesmo índice. Cada cliente gera embeddings com Ollama em loopback. Qdrant concentra documentos indexados e registros de controle. Busca e contexto exigem `program_id` e respeitam escopo aprovado, classificação e papéis. Há CLI de diagnóstico, conversão de documentos, auditoria local e releases por plataforma.

```text
Agente → hive (MCP stdio) ─ modo remoto (<center>/mind) → hive serve (HTTP + JWT) → Qdrant privado
                          └ modo local → Ollama local + Qdrant privado via TLS
```

O pacote de servidor atual implanta Qdrant e suas ferramentas de operação. Uma API central, PostgreSQL, armazenamento canônico de originais e embeddings no servidor são propostas do [plano de melhoria e limpeza](PLANO_MELHORIA_E_LIMPEZA.md), não funcionalidades já disponíveis. As specs 06–08 continuam pendentes de aceite operacional; veja [registro de execução](docs/spec/status.json).

## Usar o produto

Em uma máquina nova:

```bash
curl -fsSL https://github.com/H-I-V-E-Tec/hive_cli/releases/latest/download/install.sh | sh
hive install mind
hive login     # usuário e senha do HIVE Center
hive setup     # registra o MCP nos agentes encontrados (Claude Code, Claude Desktop, Codex)
hive doctor    # confere token, conectividade e acesso autenticado
```

O [launcher `hive`](https://github.com/H-I-V-E-Tec/hive_cli) instala e atualiza o Mind com assinatura verificada. Nenhuma URL precisa ser informada: o HIVE Center padrão (`https://hive-center.duckdns.org`) está embutido no binário e o Mind fica em `<center>/mind`. Para outro ambiente, use `--center-url`/`HIVE_CENTER_URL` (lembrado após o login) e `--mind-url`/`HIVE_MIND_URL`; fora de loopback as URLs precisam ser `https://`. Sem `HIVE_ID`, `QDRANT_URL` ou `--config`, o MCP roda em modo remoto. `hive setup <claude-code|claude-desktop|codex|all>` configura um agente específico; no Claude Code o registro é no escopo de usuário.

Os guias de instalação, operação, CLI/MCP, atualização e laboratório estão no [catálogo de guias](../guias/README.md). O guia extenso de CLI do README anterior foi preservado como [referência da implementação atual](../guias/GUIA_CLI_E_MCP.md).

`hive_mind` não contém mais `hive-data/` nem configuração operacional local. Se você desenvolve aqui, use apenas fixtures sintéticas e diretórios temporários isolados; não aponte o agente a este checkout. Consulte [AGENTS.md](AGENTS.md) antes de alterar o comportamento do produto.

## Desenvolver e verificar

Requisitos de build: Go conforme `go.mod` e compilador C/C++ para CGO/tree-sitter. A CI verifica testes Go com detector de corridas, `go vet`, scripts, scanners e imagem. Um teste rápido de código pode ser executado sem Qdrant de produção:

```bash
go test ./...
go vet ./...
```

Para build local, produza o binário em `bin/`, que é ignorado pelo Git. O [Compose](docker-compose.yml) é somente laboratório isolado e exige `HIVE_DATA_DIR` apontando para uma pasta externa explícita; não cria dados no checkout. O fluxo de distribuição aos clientes usa as releases verificadas e o [`install.sh`](install.sh).

A documentação normativa e sua situação ficam em [docs/README.md](docs/README.md). Guias operacionais foram movidos para fora do repositório; o bundle de release não depende deles. Essa pasta irmã precisa ser publicada/versionada separadamente antes que os links apareçam em um clone isolado no GitHub.
