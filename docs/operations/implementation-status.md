# Estado da implementação

Estado documental em 2026-09-11. Este arquivo separa código implementado de aceite operacional. O registro normativo em `docs/spec/status.json` continua sendo a fonte de verdade para uma spec marcada como concluída.

## Specs funcionais

| Spec | Implementação disponível | O que falta para concluir formalmente |
| --- | --- | --- |
| 00–05 | Implementadas e marcadas como concluídas no registro de specs. | Manter regressões passando. |
| 06 — Qdrant privado | TLS/CA/hostname, rejeição de HTTP remoto, papéis, validação ativa nas duas collections, Compose privado e documentação operacional. | Evidência real de TLS/VPN, JWTs individuais, revogação e writer/reader em dispositivos autorizados. |
| 07 — CLI operacional | `status`, `validate`, ingestão/prune, remoção verificada, aprovação de escopo, busca, códigos de saída e saída sanitizada. | Executar os cenários de aceite contra os serviços e a implantação reais. |
| 08 — aceite v0.1 | Controles e testes automatizados necessários foram adicionados. | Executar integralmente a matriz em duas máquinas, incluindo falhas, sincronização, reinício e recuperação. |

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
- matriz de aceite da spec 08 em duas máquinas;
- scanner dos pacotes do sistema operacional da imagem final.

O template está em `release-evidence.example.json` e o procedimento em [security-release.md](security-release.md). Enquanto qualquer item estiver pendente, o gate deve reprovar a release.
