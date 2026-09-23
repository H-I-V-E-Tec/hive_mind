# Entrega do binário e deploy do servidor

Este documento registra o fluxo implantado para separar o ciclo do cliente Hive/MCP do ciclo do Qdrant. O repositório `hive_mind` é a fonte canônica; binários não são copiados manualmente entre repositórios e uma release do cliente não reinicia o banco automaticamente.

## Fluxo resultante

```text
pull request / main
        |
        +--> security.yml --> testes, scanners, imagem e candidate temporário
        |
tag vX.Y.Z
        |
        +--> release.yml --> binários por plataforma + SBOM + checksums assinados
                         |
                         +--> hive_instance baixa e verifica o binário
                         |
                         +--> bundle hive-server-vX.Y.Z.tar.gz
                                      |
                                      +--> ambiente GitHub production
                                      +--> servidor baixa, verifica, faz backup,
                                           atualiza, testa e permite rollback
```

## Melhorias implementadas

1. Os artefatos usam o nome estável `hive-mind-vX.Y.Z-<os>-<arch>` e contêm um executável chamado `hive-mind`.
2. Releases são disparadas somente por tags semânticas `vX.Y.Z`; a tag e o commit são validados antes da compilação.
3. Commits na branch `main` geram um candidate Linux AMD64 retido por sete dias. Candidate não é release de produção e não passa pelo instalador estável.
4. A matriz cobre Linux AMD64/ARM64, macOS Intel/Apple Silicon e Windows AMD64.
5. A release inclui `SOURCE.txt`, `SHA256SUMS`, assinatura keyless Cosign, attestation de proveniência e SBOM CycloneDX.
6. Actions de terceiros estão fixadas por commit. Dependabot acompanha Go, Docker e GitHub Actions.
7. A CI executa testes Go, race detector, vet, `govulncheck`, Gitleaks, testes de backup e testes do helper administrativo/deploy.
8. A imagem Qdrant está fixada por tag e digest. O Compose possui health check interno; a promoção executa também uma consulta TLS autenticada.
9. O servidor mantém releases imutáveis em `/opt/hive-test/releases/<versão>` e promove `/opt/hive-test/current` somente depois do health check.
10. Uma atualização exige backup pareado bem-sucedido. Em falha após a troca do container, o Compose anterior é reaplicado automaticamente.
11. O deploy pelo GitHub usa um Environment `production`, aprovação manual e uma conta SSH restrita. O servidor baixa e autentica o artefato; o runner não envia código arbitrário para execução como root.
12. O `hive_instance` fixa uma versão, baixa o artefato apropriado, verifica SHA-256 e instala o binário atomicamente. Credenciais Qdrant continuam fora do Git.

## Esteiras

### Pull request e branch principal

O workflow `security.yml` é obrigatório para pull requests e pushes em branches. Configure proteção de `main` exigindo o job `Security checks / verify`, revisão e branch atualizada. O candidate gerado em `main` serve para ensaio efêmero; não deve ser copiado para produção nem referenciado pelo arquivo de versão do `hive_instance`.

### Release de produção

1. Escolha um commit aprovado em `main`, com CI de segurança verde. [Relatórios operacionais](security-release.md) e o [perfil de implantação](deployment-profile.md) são opcionais para publicar o binário; não declare ensaios pendentes como concluídos.
2. Crie e envie uma tag apontando para esse commit:

   ```bash
   git tag -a v0.2.0 -m 'Hive Mind v0.2.0'
   git push origin v0.2.0
   ```

3. `release.yml` recusa tag inválida, teste falho ou scanner falho; não verifica relatórios operacionais.
4. Atualize a versão desejada no `hive_instance` somente depois que a release existir.
5. Verifique o artefato localmente, quando necessário:

   ```bash
   cosign verify-blob \
     --bundle SHA256SUMS.sigstore.json \
     --certificate-identity \
       'https://github.com/Tiago-Balbino/hive_mind/.github/workflows/release.yml@refs/tags/v0.2.0' \
     --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
     SHA256SUMS
   sha256sum -c SHA256SUMS --ignore-missing
   ```

O operador pode publicar uma release assinada sem relatório operacional. Isso **não é autorização para promover o servidor sem backup e validação**: o deploy do Qdrant continua uma ação manual e separada, com backup, checagem de saúde e rollback. Não crie evidência fictícia apenas para publicar um binário.

### Exceção da v0.2.0: clientes Linux/macOS

A tag `v0.2.0` é imutável. Na primeira execução, os testes, a CI de segurança e os quatro builds Linux/macOS passaram, mas o build Windows falhou; por isso o job normal de assinatura/publicação foi pulado. O workflow manual `publish-client-v020.yml`, executado a partir de `main`, só pode reutilizar os quatro artefatos da execução `35867915480` depois de conferir o commit da tag e os dois jobs aprovados. Ele cria checksums, SBOM, assinatura OIDC e uma release **somente de cliente**. Windows e o bundle de servidor não são publicados nessa exceção. O `hive_instance` deve verificar a identidade exata desse workflow manual ao instalar a `v0.2.0`; releases futuras continuam usando a identidade padrão de `release.yml` na tag. Esta exceção não promove o Qdrant.

## Preparação única do servidor

Instale Docker/Compose, Python, restic, curl e Cosign `v3.1.3` por um canal verificado. Depois instale o bootstrap, que deve permanecer root-owned:

```bash
sudo install -o root -g root -m 0755 \
  deploy/pull_server_release.sh /usr/local/sbin/hive-pull-release
sudo install -o root -g root -m 0755 \
  deploy/hive-predeploy-backup.example /usr/local/sbin/hive-predeploy-backup
```

Se o repositório GitHub for privado, salve um token fine-grained somente de leitura de releases em `/srv/hive-private/github-release.token`, pertencente a root e modo `0600`. Ele nunca entra no GitHub Actions nem no repositório.

Configure `/srv/hive-private/backup.env`, root `0600`, com as variáveis do [runbook de backup](backup-recovery.md). Antes da janela, todos os writers devem estar parados ou bloqueados e o arquivo deve declarar `HIVE_BACKUP_WRITER_STOPPED=yes`. O hook executa o backup pareado e o deploy para imediatamente se ele falhar.

Para uma instalação realmente vazia:

```bash
sudo /usr/local/sbin/hive-pull-release v0.2.0 --initial
```

Não use `--initial` para adotar um servidor com dados. Migre primeiro a implantação atual para `/opt/hive-test/releases/<versão-atual>`, crie `/opt/hive-test/current` apontando para ela, configure e teste o hook de backup e somente então promova uma versão nova.

## GitHub Environment de produção

Crie o Environment `production` nas configurações do repositório, habilite reviewers obrigatórios e cadastre:

| Secret | Uso |
| --- | --- |
| `DEPLOY_HOST` | Host alcançável pelo runner. |
| `DEPLOY_PORT` | Porta SSH externa. |
| `DEPLOY_USER` | Conta exclusiva de deploy, sem shell administrativo geral. |
| `DEPLOY_SSH_KEY` | Chave privada exclusiva do workflow. |
| `DEPLOY_KNOWN_HOSTS` | Linha previamente conferida da chave do host; não use `ssh-keyscan` sem validação. |

No `sudoers`, permita a essa conta apenas o bootstrap root-owned com argumento de versão validado. A porta do servidor precisa aceitar a origem usada pelo runner; se isso não for aceitável, execute o mesmo `hive-pull-release` manualmente pelo console/VPN e mantenha o Environment desabilitado.

O workflow `deploy-server.yml` apenas solicita a promoção. O servidor baixa `hive-server-<versão>.tar.gz`, valida a assinatura OIDC do workflow de release e seu SHA-256, executa o backup, aplica o Compose, consulta a API autenticada e só então troca o link `current`.

## Rollback e dados

O rollback de software reaplica o Compose da release anterior. Os volumes Docker não são removidos e `down -v` continua proibido. Isso não reverte migrações de dados nem substitui restauração: mudanças incompatíveis exigem plano próprio e ensaio do backup pareado.

Consulte o histórico sanitizado em `/var/lib/hive-deploy/history.jsonl`. Ele contém somente versão, revisão e resultado. Logs, tokens e conteúdo não são gravados ali.

## Atualizações do `hive_instance`

O repositório cliente não deve conter executáveis compilados. Seu arquivo `hive-release.json` fixa a release e `scripts/instalar-release.sh`:

- detecta SO e arquitetura;
- baixa somente o asset correspondente;
- valida o asset contra `SHA256SUMS`;
- valida obrigatoriamente a assinatura Cosign pela política padrão da instância;
- confere `hive-mind version` após a extração;
- substitui `bin/hive-mind` atomicamente e registra a versão instalada.

Para atualizar os clientes, publique primeiro a release, altere o arquivo de versão do `hive_instance` por pull request e deixe cada máquina executar novamente `inicializar.sh`. Não atualize todos os writers antes de validar um dispositivo piloto e a compatibilidade do manifesto da collection.

## Limites operacionais ainda externos ao código

- Reviewers e regras do Environment são configurados no GitHub, não em YAML.
- A conta SSH, regra sudo, firewall, token de leitura e repositório restic são provisionados pelo operador.
- Relatórios reais e perfil operacional continuam pendentes para aceite auditado, mas não bloqueiam a release por tag.
- Antes de promover o servidor existente, é preciso migrá-lo para o layout de releases e configurar/testar o hook de backup; publicar o cliente não reinicia o Qdrant.
