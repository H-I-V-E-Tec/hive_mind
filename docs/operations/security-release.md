# Verificação operacional opcional para release

O workflow de release depende da CI de segurança, mas **não executa `cmd/security-gate` nem exige relatórios operacionais**. O dono do projeto pode publicar uma release por tag mesmo sem essas evidências. O comando abaixo é uma verificação adicional, executada por opção do operador: valida estrutura, recência e hashes dos relatórios. A veracidade dos ensaios continua sendo responsabilidade do operador e da revisão humana. Nenhum relatório pendente deve ser apresentado como aprovado.

## Preparar evidências, se desejar uma verificação auditada

1. Preencha o [perfil operacional](deployment-profile.md): responsável acionável, canal privado, RPO/RTO, retenções e writer/reader autorizados. Nenhum `UNSET` pode permanecer na aprovação.
2. Execute os ensaios no código candidato e registre resultados sanitizados em `docs/operations/evidence/`. Não inclua tokens, documentos, queries reais, certificados privados ou paths absolutos. Esses arquivos serão versionados e não são um cofre.
3. Copie `release-evidence.example.json` para `release-evidence.json` e preencha versão, data UTC, controles, parâmetros e cada gate. Use `passed` somente com o ensaio concluído, caminho local do relatório e seu SHA-256.
4. Inclua os arquivos de produto/testes/configuração novos no índice Git antes de calcular o digest. `--digest` considera arquivos rastreados e seu conteúdo atual, não arquivos novos ainda não adicionados. O relatório final e a pasta `evidence/` são excluídos do digest para evitar hash circular.

```bash
go run ./cmd/security-gate --digest
sha256sum docs/operations/evidence/<relatorio>.md
go run ./cmd/security-gate --version v0.1.0
```

5. Registre o primeiro resultado como `source_digest`. O verificador opcional exige a mesma versão solicitada, evidências com até 30 dias, parâmetros positivos, carência de prune até 24 horas e retenção de auditoria de 30 dias (política implementada).
6. Revise e faça commit do candidato e evidências. No checkout usado para a verificação auditada, o digest deve continuar idêntico. Alterar produto, README, testes, deployment ou pipeline invalida a vinculação anterior. Não recalcule o digest para reutilizar ensaios que já não verificam o código alterado.

## Controles avaliados no modo auditado

| Gate | Evidência mínima |
| --- | --- |
| `writer_reader_validation` | Validação real de cada writer (registro próprio, escrita) e de cada reader (leitura, escrita negada) nas duas collections. |
| `private_tls_network` | Firewall/VPN, TLS válido, CA errada rejeitada e ausência de exposição pública. |
| `encryption_at_rest` | Discos de documentos, Qdrant, staging e auditoria criptografados; acesso restrito. |
| `individual_revocation` | Revogação de um dispositivo bloqueia seu acesso sem reutilização de segredo. |
| `writer_promotion` | Admissão de um writer novo com approval id e token próprios; revogação de um writer sem afetar os demais nem seus documentos; writer não registrado reprovado em `validate`. |
| `credential_rotation` | Token novo funciona e antigo deixa de funcionar pelo mecanismo efetivo da implantação. |
| `paired_restore` | Ensaio de restauração das duas collections com busca/escopo/revisões e RPO/RTO medidos. |
| `audit_privacy` | Ausência de conteúdo/segredos nos logs, permissões, rotação, expiração e disco indisponível. |
| `two_machine_acceptance` | Cenários da spec 08 executados em máquinas autorizadas, incluindo os passos multi-writer; o [ensaio de quatro pessoas](four-person-trial.md) é a execução de referência. |
| `container_verification` | Imagem final, usuário não root, limites, mounts, segredos e scanner de vulnerabilidades de SO. |

Mapeie cada ameaça de `threat_controls` para controle e teste/relatório verificável. Resultados de scanner devem registrar versões e data. A CI Go não substitui um scanner dos pacotes da imagem, e um teste com mocks não substitui TLS/VPN ou restauração real.

A release inclui versão/revisão no binário, arquivo `SOURCE.txt` e `SHA256SUMS`. Tag existente apontando para outro commit bloqueia publicação. Alterações dos workflows devem passar pela proteção/revisão do repositório. O relatório opcional não substitui governança de acesso nem os controles reais da implantação.
