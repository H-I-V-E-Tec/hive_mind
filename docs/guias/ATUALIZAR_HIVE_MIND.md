# Atualizar o Hive Mind, distribuir aos clientes e operar o servidor

Este guia acompanha uma mudança concreta: adicionar uma ferramenta MCP ao `hive_mind` e disponibilizá-la às pessoas que usam `hive_instance`. Use `v0.2.0` apenas como exemplo; substitua pela próxima versão aprovada.

## Entenda o que roda em cada lugar

```text
hive_mind (código Go) --release--> binário no hive_instance de cada pessoa
                                           |
                              MCP local + Ollama local
                                           |
                                    túnel SSH/TLS
                                           |
                              Qdrant no servidor Ubuntu
```

O `hive_mind` é o código-fonte. O `hive_instance` guarda a configuração de cada cliente e a **versão desejada** em `hive-release.json`; o executável fica em `bin/hive-mind` apenas na máquina da pessoa. O processo MCP é iniciado localmente pelo cliente de IA via `bin/hive-teste`. O servidor Ubuntu hospeda o Qdrant, suas collections e o helper administrativo; ele não executa a ferramenta MCP para as pessoas.

| Mudança | Publicar release do `hive_mind` | Atualizar `hive_instance` e clientes | Fazer deploy do Qdrant |
| --- | --- | --- | --- |
| Nova ferramenta MCP ou correção na busca | Sim | Sim | Não, se o contrato de dados continua compatível |
| Alteração apenas no guia ou no script do `hive_instance` | Não | Sim | Não |
| Imagem/Compose/TLS/helper do Qdrant | Sim, para gerar o bundle assinado | Só se a configuração do cliente também mudar | Sim |
| Parser, dimensão do embedding ou schema da collection | Sim | Sim, após plano de migração | Pode exigir migração de dados; siga o contrato específico |
| Cadastro de uma pessoa nova | Não | A pessoa instala a versão já fixada | Não; há apenas criação de conta SSH e emissão de JWT |

Uma tag do `hive_mind` publica tanto binários de cliente quanto um bundle de servidor. A existência do bundle **não aciona deploy**: o workflow de servidor é manual e separado. Atualizar `hive-release.json` também não instala nada por si só; cada computador precisa executar o instalador.

## 1. Desenvolver uma ferramenta MCP nova

Na raiz do repositório `hive_mind`, use `hive_search` e `hive_get_context` em `server/mcp.go` como exemplos. Para uma nova ferramenta chamada, por exemplo, `hive_resumo`:

1. Defina o contrato de entrada e saída e a permissão: reader, writer ou ambos. Preserve os limites de `hive_id`, `program_id`, classificação e escopo pertinentes à operação.
2. Adicione o nome, a descrição e o `inputSchema` em `availableTools()`; se houver saída estruturada, publique também o `outputSchema`.
3. Adicione o tratamento de `tools/call` em `handleMCPMethod()`. Valide os argumentos, execute a lógica da ferramenta e retorne erros/resultados no formato MCP. A lógica de negócio deve ficar em funções próprias, com testes.
4. Teste `tools/list`, uma chamada válida, argumentos inválidos e a visibilidade por papel. Se a ferramenta escreve, teste que readers não a veem nem conseguem executá-la.
5. Atualize README, templates de instrução/skill e contratos afetados. Uma ferramenta nova só aparece no cliente de IA quando ele inicia o **binário novo** e faz novamente `tools/list`.

Verificação local mínima, ainda no `hive_mind`:

```bash
go test ./...
go vet ./...
go build -trimpath -o bin/hive-mind .
./bin/hive-mind help
```

Para enviar a mudança à revisão, use uma branch própria. Adicione também os outros arquivos que você alterou, sempre revisando a seleção antes do commit:

```bash
git switch -c feat/hive-resumo
git add server/mcp.go server/mcp_test.go
git status --short
git commit -m 'Adiciona ferramenta MCP hive_resumo'
git push -u origin feat/hive-resumo
```

Abra um pull request dessa branch para `main`. A CI roda testes, scanners e validação da imagem. Um push em `main` produz um candidate temporário para ensaio; o candidate não é a release que o `hive_instance` instala. Só crie a tag depois do merge e da aprovação das evidências.

## 2. Publicar a versão do `hive_mind`

Depois que o código e a documentação estiverem estáveis, siga o [gate de segurança](../operations/security-release.md): preencha o [perfil operacional](../operations/deployment-profile.md), execute os ensaios reais, grave evidências sanitizadas e vincule `source_digest` aos arquivos candidatos já adicionados ao índice Git. A release só é publicada se o gate e a CI passarem. Não há, neste momento, `release-evidence.json` preenchido nem uma versão aprovada fixada no `hive_instance`.

Com o commit aprovado em `main`, crie uma tag nova. Se você configurou assinatura de tags Git, pode usar `git tag -s`; uma tag anotada também é aceita pelo workflow atual:

```bash
git tag -a v0.2.0 -m 'Hive Mind v0.2.0'
git push origin v0.2.0
```

O workflow `Build and Release` valida a tag e o commit, executa o gate, compila os binários, cria o bundle `hive-server-v0.2.0.tar.gz`, o SBOM, `SHA256SUMS` assinado e publica a GitHub Release. Confira que a execução terminou com sucesso e que o asset para a plataforma do cliente existe antes de seguir. Não reutilize uma tag apontando para outro commit.

## 3. Atualizar o `hive_instance`

No repositório separado `hive_instance`, para uma ferramenta MCP que não exige novos parâmetros de configuração, altere **somente o pin da versão** em `hive-release.json`; não é necessário copiar o código da ferramenta para lá:

```json
{
  "schema_version": 1,
  "repository": "Tiago-Balbino/hive_mind",
  "version": "v0.2.0",
  "require_signature": true
}
```

Abra um pull request nesse repositório, confira sua CI e publique a mudança. Se a release ainda não existe, mantenha `UNSET`: o instalador falha de forma explícita. Nunca copie executáveis compilados para o Git do `hive_instance`. Também não copie `writer.jwt`, `ca.crt` ou um token de leitura do GitHub para o repositório.

O instalador usa a versão fixada para escolher Linux/macOS e arquitetura, baixa `hive-mind-v0.2.0-<os>-<arch>.tar.gz`, verifica SHA-256 e assinatura Cosign, confere `hive-mind version` e substitui `bin/hive-mind` atomicamente. `bin/hive-teste` continua apontando para esse mesmo caminho. Se a ferramenta precisar de nova variável, identidade, opção do launcher ou instrução MCP, atualize também o script/template correspondente no `hive_instance`, teste a inicialização e só então publique seu PR. Se a release for privada, cada máquina precisa de `config/github-release.token` com acesso de leitura e modo `0600`.

## 4. Baixar a atualização em um cliente existente

Na máquina de **uma pessoa piloto**, na raiz do `hive_instance`:

```bash
git pull --ff-only
bash scripts/instalar-release.sh
bin/hive-mind version
```

O campo `version` deve mostrar `v0.2.0`; `source_revision` identifica o commit da release. O instalador não troca a identidade, o JWT, a CA, o TOML nem os documentos locais. O `git pull` pode precisar de resolução manual se a pessoa editou um arquivo versionado, como `scripts/abrir-tunel.sh`.

Com o túnel SSH e o Ollama locais em execução, confira:

```bash
bin/hive-teste validate
bin/hive-teste status
```

Reinicie ou reconecte o servidor MCP `hive_mind` no Codex/Claude Code e confira se `tools/list` mostra `hive_resumo` e se uma chamada controlada funciona. Uma sessão MCP que já estava aberta continua usando o processo anterior até ser reiniciada. Depois de validar a pessoa piloto, repita os comandos nas demais máquinas. Atualizar o cliente não exige reemitir JWT nem cadastrar o writer novamente se sua identidade e os contratos persistidos continuam iguais.

Para atualizar também os scripts/configuração inicial de uma máquina, execute `bash scripts/inicializar.sh <mesmo-nome-usado-no-cadastro>`. Em clientes já registrados, prefira o instalador acima quando só o binário mudou: `inicializar.sh` regenera `config/hive.toml` e os apontamentos MCP a partir do nome informado.

## 5. Quando e como fazer deploy no servidor

Para a ferramenta `hive_resumo` que apenas usa as collections existentes, **pare após o passo 4**. O servidor Qdrant não recebe o binário MCP e não precisa ser reiniciado.

Se a mudança altera a imagem do Qdrant, Compose, TLS ou helper administrativo, use a mesma release aprovada para o servidor. Primeiro configure o [bootstrap e o backup](../operations/delivery-and-deployment.md#preparação-única-do-servidor). Em uma atualização, pare/bloqueie os writers conforme o runbook, confirme o backup pareado e acione o workflow `Deploy Qdrant server` com `version=v0.2.0` e `initial=false`. O Environment `production` requer aprovação e secrets SSH configurados no GitHub.

Se o GitHub Actions não tiver acesso SSH ao host, execute pelo console/VPN do servidor, com o bootstrap já instalado:

```bash
sudo /usr/local/sbin/hive-pull-release v0.2.0
```

O script baixa e verifica o bundle assinado, exige um hook de backup para atualizar uma instalação existente, aplica o Compose, consulta a API TLS com a credencial administrativa e só então aponta `/opt/hive-test/current` para a nova release. Se o health check falhar após iniciar o container, ele reaplica o Compose anterior. Os volumes Qdrant não são removidos. Esse rollback de software não reverte uma migração de dados; teste migrações em isolamento antes de promovê-las.

Verifique a versão ativa e a saúde depois da promoção:

```bash
readlink -f /opt/hive-test/current
sudo docker compose -p hive-server \
  -f /opt/hive-test/current/deploy/qdrant-server.compose.yml ps
sudo python3 /opt/hive-test/current/deploy/qdrant_admin.py health
```

`--initial` só serve para um servidor realmente vazio. Para adotar um Qdrant já existente, siga primeiro o procedimento de migração da implantação no [guia de entrega](../operations/delivery-and-deployment.md#preparação-única-do-servidor). Não execute `down -v`.

## 6. O que muda ao cadastrar uma pessoa nova

Uma pessoa nova clona o `hive_instance` já atualizado e instala a **mesma versão** fixada em `hive-release.json`. O código da ferramenta não é compilado na máquina dela nem enviado ao Qdrant. O cadastro tem três partes independentes:

1. **Acesso à rede:** o operador cria uma conta SSH de túnel, registra a chave pública da pessoa e confere o firewall da porta `20004`.
2. **Acesso aos dados:** no servidor, o operador emite um JWT individual para o `device_id` e entrega somente o `writer.jwt` dessa pessoa e a CA existente por canal privado. Não gere outra CA nem compartilhe o token de outro dispositivo.
3. **Cliente local:** a pessoa coloca os arquivos em `hive_instance/config/`, executa `bash scripts/inicializar.sh bruno`, abre o túnel como `hive-bruno` e, como writer, executa uma primeira vez `bin/hive-teste ingest`. Essa primeira ingestão registra o writer mesmo se ainda não houver documentos. Depois roda `validate` e `status` e reinicia o cliente MCP.

O JWT deve ser emitido para `bruno-laptop`, exatamente o `device_id` gerado por `inicializar.sh bruno`; a configuração também gera `teste-001-bruno-laptop` como approval ID. A pessoa nova já recebe a ferramenta `hive_resumo` ao instalar a release fixada. O cadastro dela não exige uma nova tag, migração ou reinício do Qdrant.

O script atual `scripts/abrir-tunel.sh` contém `SSH_USER="ubuntu"` fixo. Para Bruno, ajuste esse valor apenas na cópia local para `hive-bruno` ou abra o túnel diretamente com os mesmos parâmetros SSH; não suponha que o nome passado ao inicializador altere o túnel. Uma edição local do script versionado deve ser preservada ao fazer futuros `git pull`.

O procedimento detalhado de conta SSH, emissão, entrega e revogação está em [Adicionar uma pessoa no servidor](ADICIONAR_PESSOA_NO_SERVIDOR.md). Para um reader, configure papel e credencial de leitura conforme o [guia multiusuário](../operations/server-multiuser-guide.md); o inicializador atual do `hive_instance` configura `writer`.

## 7. Se algo falhar

| Sintoma | Verificação inicial |
| --- | --- |
| A tag foi enviada, mas não há release | Veja os jobs `security` e `test_and_verify`; perfil/evidências ausentes bloqueiam a publicação. |
| `hive-release.json` ainda tem `UNSET` | Publique uma release aprovada e fixe sua versão; nenhuma instalação nova será baixada antes disso. |
| Download falha ou retorna 404 | Confira tag/asset publicado e, se o repositório é privado, o token de leitura local. |
| SHA-256 ou Cosign falha | Interrompa a instalação e confira asset, assinatura e origem; o binário anterior permanece no lugar. |
| `version` mudou, mas a ferramenta não aparece | Reinicie a conexão MCP e confirme que ela executa `bin/hive-teste` desta instância. |
| `validate` falha após a atualização | Confira túnel, Ollama, expiração do JWT e compatibilidade de modelo/schema. |
| Deploy do servidor falha | Consulte o resultado do backup e o health check; o link `current` só muda após sucesso. |

Para voltar à versão anterior do cliente, fixe a release anterior em `hive-release.json` e execute `scripts/instalar-release.sh` de novo, após confirmar compatibilidade com a collection atual. Para o servidor, use o [runbook de backup e recuperação](../operations/backup-recovery.md) caso exista mudança de dados.
