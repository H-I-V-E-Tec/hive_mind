# Estado da implementação

Estado documental em 2026-09-12. Este arquivo separa código implementado de aceite operacional. O registro normativo em `docs/spec/status.json` continua sendo a fonte de verdade para uma spec marcada como concluída.

Atualização 2026-09-15: primeira entrega do plano de melhoria implementa [relatório de ingestão v1](../spec/contracts/ingestion-report.md) em CLI/MCP, com resultados por arquivo, inalterados e falhas parciais preservadas. Suíte completa, `go vet` e testes de ingestão/revisão com detector de corridas passaram; specs 06–08 continuam pendentes de aceite operacional. O andamento está na [seção 16 do plano](../../plano%20de%20melhoria.md#16-andamento-da-implementação).

## Specs funcionais

Segunda entrega do plano (2026-09-15): `inventory` permite diagnosticar o corpus local sem serviços. A [linha de base](inventory-baseline.md) registra cobertura de 100 arquivos em cópia controlada e somente repetições de arquivos vazios. Há corpus sintético rotulado e proposta de contrato do conversor; novos adaptadores e deduplicação canônica ainda não estão ativos.

Terceira entrega do plano (2026-09-16): pendências da Fase 0 fechadas. [Linha de base de recuperação](retrieval-baseline.md) reproduzível por `go test` ([contrato](../spec/contracts/eval-report.md)), com Qdrant em memória com scoring e embedder sintético — mede efeitos estruturais do pipeline, não o modelo real. Contrato da [unidade canônica](../spec/contracts/canonical-unit.md) e [decisão 003](../decisions/003-canonical-persistence.md) (banco canônico compartilhado) registrados; nenhum campo é persistido ainda. README, help e TODO reconciliados com o código. O subcomando `evaluate-search`, inalcançável desde a introdução do `splitCLIArgs`, foi removido; contadores de embedding (`SnapshotEmbeddingStats`) foram adicionados ao worker sem alterar o relatório de ingestão v1.

| Spec | Implementação disponível | O que falta para concluir formalmente |
| --- | --- | --- |
| 00–05 | Implementadas e marcadas como concluídas no registro de specs. 00 e 02 estão na versão 1.1.0 (multi-writer com propriedade por documento e por aprovação de escopo). | Manter regressões passando; executar os passos multi-writer da spec 08 em máquinas reais. |
| 06 — Qdrant privado | TLS/CA/hostname, rejeição de HTTP remoto, papéis, validação ativa nas duas collections, Compose privado e documentação operacional. | Evidência real de TLS/VPN, JWTs individuais, revogação e writer/reader em dispositivos autorizados. |
| 07 — CLI operacional | `status`, `validate`, ingestão/prune, remoção verificada, aprovação de escopo, busca, códigos de saída e saída sanitizada. | Executar os cenários de aceite contra os serviços e a implantação reais. |
| 08 — aceite v0.1 | Controles e testes automatizados necessários foram adicionados, incluindo os passos multi-writer. | Executar integralmente a matriz em máquinas reais — o [ensaio de quatro pessoas](four-person-trial.md) é a execução de referência — incluindo falhas, sincronização, reinício e recuperação. |
| 09 — métricas de uso | `response_chars`, `source_bytes` e `truncated` nos eventos de recuperação; `document_bytes` no head; `audit report`. Concluída com testes. | Coletar relatórios reais no ensaio e registrar a economia medida. |
| 10 — integração de agentes | Templates `claude` e `codex` reescritos para recon; `install-skill claude` novo. Concluída com testes. | Validar em sessões reais que os agentes seguem o fluxo e escrevem notas válidas. |

As specs 06–08 permanecem `pending` porque testes unitários/mocks não provam propriedades da infraestrutura real. Elas só devem mudar para `completed` junto de evidência reproduzível, data, revisão e commit.

## Pendências operacionais (2026-09-16)

Registro consolidado do que a Fase 0 identificou e não resolveu; cada item aponta a fase do plano que o cobre.

| Pendência | Situação | Onde |
| --- | --- | --- |
| Specs 06–08 | `pending`; exigem evidência em infraestrutura real. | Ensaio de quatro pessoas |
| Retenções numéricas | Todas `UNSET` em [deployment-profile.md](deployment-profile.md); a decisão 003 acrescenta banco canônico e fila de revisão à lista. | Antes do MVP do conversor |
| Modo de busca | `SearchMode` fixo em `dense`; híbrido implementado, não configurável nem exposto em `status`. | Fase 1 |
| Documentos `classification: unknown` | Ingeridos e embedados, nunca devolvidos pela busca (fail-closed). README agora avisa; falta diagnóstico no relatório de ingestão. | Fase 1 |
| Cota por documento no top-k | Um documento pode ocupar todos os resultados; medido na linha de base. | Fase 1 (avaliação de ranking/diversidade) |
| Prefixo repetido por chunk | 34% do texto indexado nas fixtures; front matter + título em cada seção. | Fase 2 (normalização) |
| Rename no watcher | Não tratado; `ingest` reconcilia ausentes com carência. | Fase 1 |
| Identidade CLI/MCP | `serverInfo.name = go-qdrant-sync-mcp`; usages com `qdrant-mcp-server`. | Fase 1 |
| Arquivo acima de 5 MiB no acervo | `recon__urls.txt` (~8,4 MB) depende de `HIVE_MAX_FILE_BYTES`; é `.txt` sem classificação, logo não seria pesquisável mesmo ingerido. | Operador + Fase 1 |
| Validação e2e de `validate`/`status` e autostart de serviços | Dependem do laboratório do operador. | Operador |
| Medição com modelo real | O harness aceita transporte HTTP real; ainda não foi executado contra Ollama/Qdrant. | Fase 1 |

## Segurança

- Auditoria estruturada e sanitizada, escrita durável, diretório privado, rotação e retenção de 30 dias; falha bloqueia operações críticas.
- CI com race detector, vet, verificação de módulos, govulncheck, Gitleaks em arquivos/histórico, testes do backup e build/inspeção da imagem.
- Dependências vulneráveis alcançáveis identificadas durante a implementação foram atualizadas; o resultado precisa ser reavaliado a cada mudança e release.
- Runtime em container não root, raiz somente leitura no Compose, capabilities removidas, limites de recursos e mounts restritos.
- Backup pareado de dados/controle com hashes, restic criptografado, auditoria e restore para staging sem sobrescrever produção.
- Gate de release fail-closed vincula evidências operacionais atuais ao digest do código candidato.

## Evidências ainda obrigatórias

Continuam dependentes do operador/ambiente e não foram declaradas como concluídas:

- owner, canal de incidente, RPO, RTO e retenções reais;
- tráfego privado com TLS, CA inválida rejeitada e ausência de exposição pública;
- criptografia em repouso de documentos, Qdrant, staging e auditoria;
- admissão, rotação, revogação individual e promoção de writer;
- backup/restauração das duas collections em Qdrant isolado, com RPO/RTO medidos;
- auditoria de privacidade e indisponibilidade real do disco de logs;
- matriz de aceite da spec 08 em máquinas reais, incluindo dois ou mais writers;
- relatórios `audit report` consolidados e comparação de tokens com/sem Hive (spec 09);
- scanner dos pacotes do sistema operacional da imagem final.

O template está em `release-evidence.example.json` e o procedimento em [security-release.md](security-release.md). Enquanto qualquer item estiver pendente, o gate deve reprovar a release.
