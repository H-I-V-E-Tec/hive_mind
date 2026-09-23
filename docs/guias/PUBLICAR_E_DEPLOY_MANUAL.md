# Publicar uma versão e atualizar os clientes

Este é o roteiro operacional para publicar o Hive Mind sem depender de outra pessoa. A release por tag não exige relatórios operacionais; ela exige que a CI de segurança e os testes passem. O Qdrant não é reiniciado pela publicação do cliente.

## Publicação especial da v0.2.0

A tag `v0.2.0` é imutável. Ela é uma publicação somente para Linux e macOS; Windows ficou fora porque o build dessa plataforma falhou. O workflow manual recompila os quatro clientes diretamente da tag e assina os checksums.

Depois de a PR de correção estar incorporada à `main`:

1. Abra **Actions** no GitHub.
2. Selecione **Publish v0.2.0 clients (Linux and macOS)**.
3. Clique em **Run workflow**, escolha `main` e confirme.
4. Aguarde os quatro jobs `Build (...)` e o job `Sign and publish client assets`.
5. Confirme a release em `https://github.com/H-I-V-E-Tec/hive_mind/releases/tag/v0.2.0`.

Também é possível disparar pela API. Use um token com permissão de Actions; mantenha-o apenas na variável local:

```bash
export GITHUB_TOKEN='seu-token-com-permissão-de-actions'
curl --fail-with-body -L \
  -X POST \
  -H "Accept: application/vnd.github+json" \
  -H "Authorization: Bearer $GITHUB_TOKEN" \
  -H "X-GitHub-Api-Version: 2022-11-28" \
  https://api.github.com/repos/H-I-V-E-Tec/hive_mind/actions/workflows/publish-client-v020.yml/dispatches \
  -d '{"ref":"main"}'
unset GITHUB_TOKEN
```

O retorno `204` significa que o disparo foi aceito; ainda é necessário acompanhar a execução e confirmar a release. Se falhar, não existe release utilizável para o cliente.

## Atualizar o `hive_instance`

No clone do `hive_instance`, altere somente o pin:

```json
{
  "schema_version": 1,
  "repository": "Tiago-Balbino/hive_mind",
  "version": "v0.2.0",
  "require_signature": true
}
```

Faça commit por pull request e, depois do merge, em cada máquina cliente execute:

```bash
bash scripts/inicializar.sh nome-do-dispositivo
bin/hive-mind version
```

O instalador baixa o asset da plataforma, confere SHA-256, confere a assinatura Cosign e instala o binário atomicamente. A `v0.2.0` usa a identidade do workflow manual `publish-client-v020.yml` sob o proprietário antigo. Ao fixar uma release nova, altere `repository` para `H-I-V-E-Tec/hive_mind` e `version` para a nova tag no mesmo PR; ela usa a identidade do workflow normal de release sob o novo proprietário.

## Deploy do Qdrant no servidor

Publicar o cliente não faz deploy do servidor. Só use **Actions → Deploy Qdrant server** quando o servidor estiver preparado com `/opt/hive-test/releases`, `current`, hook de backup e acesso SSH do Environment `production`.

Na primeira instalação vazia, marque `initial`; em servidor com dados, deixe `initial` desmarcado. O servidor deve fazer backup, verificar a assinatura do bundle, aplicar o Compose, testar a saúde e só então promover `current`. O servidor atual ainda precisa ser migrado para esse layout; não execute o workflow sobre ele como se fosse uma instalação vazia.

## Próximas versões

Não mova nem reutilize `v0.2.0`. Para uma correção, use `v0.2.1`; para uma nova funcionalidade compatível, `v0.3.0`. Depois do merge em `main`:

```bash
git tag -a v0.2.1 -m 'Hive Mind v0.2.1'
git push origin v0.2.1
```

O workflow normal `Build and Release` executa testes, scanners, matriz de builds, assinatura e publicação. Depois de a release existir, atualize o pin do `hive_instance` por pull request.
