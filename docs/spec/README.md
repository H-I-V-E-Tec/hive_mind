# Specs do Hive Mind v0.1

Estas specs definem um novo produto Hive Mind. Nada nelas exige compatibilidade com o servidor RAG que originou o repositório.

| Etapa | Resultado |
| --- | --- |
| [00](00-system-invariants.md) | Topologia, papéis, confiança e consistência da v0.1. |
| [01](01-hive-configuration.md) | Configuração exclusiva e segura do Hive Mind. |
| [02](02-document-ingestion.md) | Ingestão de documentos de recon com proveniência. |
| [03](03-recon-metadata.md) | Metadados de escopo e recon filtráveis. |
| [04](04-filtered-search.md) | Busca MCP isolada por Hive e programa. |
| [05](05-hive-context.md) | Contexto compacto que prioriza escopo. |
| [06](06-private-qdrant.md) | Transporte e acesso ao Qdrant protegidos. |
| [07](07-operational-cli.md) | Operação, validação e diagnóstico seguros. |
| [08](08-v01-acceptance.md) | Aceitação ponta a ponta em duas máquinas. |

## Segurança transversal

As [specs de segurança](security/README.md) detalham os controles obrigatórios para todas as etapas. Uma funcionalidade que manipule dados, rede, credenciais ou respostas MCP só pode ser concluída quando também cumprir a spec de segurança correspondente.

Cada implementação deve seguir as decisões [001](../decisions/001-tests-are-required.md) e [002](../decisions/002-security-is-primary.md), atualizar o README quando mudar o produto e manter `go test ./...` verde.

As etapas são dependentes: nenhuma implementação pode contrariar as invariantes da etapa 00. Em especial, a v0.1 usa writer único, autorização de escopo aprovada fora do conteúdo comum e publicação por revisão recuperável.
