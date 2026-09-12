# Estado da implementação

Estado documental em 2026-09-12. Este arquivo separa código implementado de aceite operacional. O registro normativo em `docs/spec/status.json` continua sendo a fonte de verdade para uma spec marcada como concluída.

## Specs funcionais

| Spec | Implementação disponível | O que falta para concluir formalmente |
| --- | --- | --- |
| 00–05 | Implementadas e marcadas como concluídas no registro de specs. 00 e 02 estão na versão 1.1.0 (multi-writer com propriedade por documento e por aprovação de escopo). | Manter regressões passando; executar os passos multi-writer da spec 08 em máquinas reais. |
| 06 — Qdrant privado | TLS/CA/hostname, rejeição de HTTP remoto, papéis, validação ativa nas duas collections, Compose privado e documentação operacional. | Evidência real de TLS/VPN, JWTs individuais, revogação e writer/reader em dispositivos autorizados. |
| 07 — CLI operacional | `status`, `validate`, ingestão/prune, remoção verificada, aprovação de escopo, busca, códigos de saída e saída sanitizada. | Executar os cenários de aceite contra os serviços e a implantação reais. |
| 08 — aceite v0.1 | Controles e testes automatizados necessários foram adicionados, incluindo os passos multi-writer. | Executar integralmente a matriz em máquinas reais — o [ensaio de quatro pessoas](four-person-trial.md) é a execução de referência — incluindo falhas, sincronização, reinício e recuperação. |
| 09 — métricas de uso | `response_chars`, `source_bytes` e `truncated` nos eventos de recuperação; `document_bytes` no head; `audit report`. Concluída com testes. | Coletar relatórios reais no ensaio e registrar a economia medida. |
| 10 — integração de agentes | Templates `claude` e `codex` reescritos para recon; `install-skill claude` novo. Concluída com testes. | Validar em sessões reais que os agentes seguem o fluxo e escrevem notas válidas. |

As specs 06–08 permanecem `pending` porque testes unitários/mocks não provam propriedades da infraestrutura real. Elas só devem mudar para `completed` junto de evidência reproduzível, data, revisão e commit.

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
