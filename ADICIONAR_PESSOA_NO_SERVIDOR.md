# Administrador: adicionar mais uma pessoa ao Hive

A instância já está pronta. Use este roteiro a cada nova pessoa: criar a conta de túnel, gerar o par de chaves SSH, cadastrar a chave pública, emitir um JWT e preparar os arquivos para entrega. A CA existente é reutilizada.

**Fluxo escolhido para este teste: entrega privada por pessoa.** O administrador emite e entrega `writer.jwt` individualmente; o participante salva o arquivo em `hive_instance/config/` e executa o inicializador. A mesma `ca.crt` acompanha cada entrega. O certificado da CA é público, mas sua origem/impressão digital precisa ser conferida; o JWT é a credencial secreta.

O download automático pode ser implementado com autenticação e autorização por destinatário. Para o grupo atual, a entrega privada aproveita o inicializador existente e evita criar um serviço de distribuição de credenciais. Apenas informar o nome não autentica o destinatário. A chave administrativa do Qdrant permanece com o operador; os participantes usam tokens limitados às collections do Hive, conforme o modelo de [acesso granular do Qdrant](https://qdrant.tech/documentation/security/#granular-access-api-keys).

Este guia foi ajustado após leitura de `../hive_instance/scripts/inicializar.sh`, `scripts/abrir-tunel.sh` e `bin/hive-teste`, em 18/09/2026. Na cópia local analisada, o inicializador **procura as credenciais no disco**, dentro de `hive_instance/config/`; ele não baixa CA/JWT do servidor. A busca remota desses arquivos ainda não aparece nessa versão do script.

| Comportamento conferido no código | Consequência para o cadastro |
| --- | --- |
| `inicializar.sh bruno` gera `bruno-laptop` e `teste-001-bruno-laptop`. | Emita o JWT para `bruno-laptop` e use exatamente `bruno` ao inicializar. |
| Exige `config/writer.jwt` e `config/ca.crt` já presentes. | Entregue esses arquivos antes de executar a inicialização. |
| Usa diretórios dentro da raiz de `hive_instance`. | O destino das credenciais é `hive_instance/config/`, não `~/hive-teste/config/`. |
| `abrir-tunel.sh` fixa `atlasgrid.site:20004`, usuário `ubuntu`. | A cópia administrativa usa essa porta. Para o participante, ajuste o usuário do túnel para `hive-bruno`. |
| O nome passado ao inicializador não altera `abrir-tunel.sh`. | Esse ajuste do usuário SSH precisa ser feito separadamente na versão atual. |

## 1. No servidor: definir quem será adicionado

Entre com sua conta administrativa e vire root **antes** de definir as variáveis, porque `sudo -i` abre um shell novo e descarta variáveis definidas antes dele:

```bash
sudo -i
id -u
```

`id -u` deve imprimir `0`. Se imprimir outro número, você não é root: os comandos `adduser`, `usermod` e `install` dos próximos passos falham com `Only root may add a user or group to the system` e `invalid user`. Continue somente com `0`.

Altere `HIVE_ADD_PERSON` e `HIVE_ADD_ADMIN` abaixo; o device ID é derivado do nome, como faz o inicializador. O exemplo adiciona Bruno e usa `ubuntu` como sua conta administrativa para copiar arquivos. Se você acessa o servidor como `root`, troque `ubuntu` por `root`.

```bash
HIVE_ADD_PERSON=bruno
HIVE_ADD_DEVICE="${HIVE_ADD_PERSON}-laptop"
HIVE_ADD_ADMIN=ubuntu
HIVE_ADD_SSH="hive-${HIVE_ADD_PERSON}"
HIVE_ADD_COLLECTION=hive_teste_4p
cd /opt/hive-test
umask 077
```

Use identificadores minúsculos, sem espaços; o nome curto deve começar com letra e ter até 20 caracteres. Evite pontos: a normalização do inicializador os aceita, mas a validação de identidade do Hive não. Mantenha esta sessão aberta, pois os próximos comandos usam essas variáveis. A collection deve ser a mesma da instalação existente.

## 2. No servidor: criar a conta de túnel

Ainda na mesma sessão root do passo 1 (`id -u` = `0`, variáveis definidas):

```bash
[ "$(id -u)" -eq 0 ] || { echo "ERRO: execute sudo -i e refaça o passo 1"; false; }
if ! id "$HIVE_ADD_SSH" >/dev/null 2>&1; then
  adduser --disabled-password --gecos '' "$HIVE_ADD_SSH"
fi
usermod -aG hive-tunnel "$HIVE_ADD_SSH"
install -d -m 700 -o "$HIVE_ADD_SSH" -g "$HIVE_ADD_SSH" \
  "/home/${HIVE_ADD_SSH}/.ssh"
if [ ! -e "/home/${HIVE_ADD_SSH}/.ssh/authorized_keys" ]; then
  install -m 600 -o "$HIVE_ADD_SSH" -g "$HIVE_ADD_SSH" /dev/null \
    "/home/${HIVE_ADD_SSH}/.ssh/authorized_keys"
fi
id "$HIVE_ADD_SSH"
```

Se a primeira linha imprimir `ERRO`, rode `sudo -i` e repita o passo 1 inteiro (as variáveis precisam ser definidas dentro do shell root). O grupo `hive-tunnel` já deve existir. O comando preserva chaves cadastradas anteriormente. No resultado de `id`, confira se `hive-tunnel` aparece entre os grupos.

## 3. No computador de administrador: gerar o par de chaves SSH da pessoa

**Fora do SSH, no seu computador de administrador.** Neste teste o administrador gera o par de chaves da nova pessoa: a **chave pública** é cadastrada no servidor (passo 4) e a **chave privada** é entregue somente à pessoa junto com o JWT (passo 7). Se a pessoa já tiver uma chave própria e preferir enviar a pública, pule este passo e use a linha que ela enviar no passo 4.

```bash
umask 077
mkdir -p "$HOME/hive-entregas/bruno-laptop"
chmod 700 "$HOME/hive-entregas/bruno-laptop"
ssh-keygen -t ed25519 -a 64 -C "hive-bruno@bruno-laptop" \
  -f "$HOME/hive-entregas/bruno-laptop/hive_servidor"
```

Ao pedir a passphrase, pressione Enter duas vezes para deixá-la vazia: `abrir-tunel.sh` usa a chave diretamente e não pede senha. Se a pessoa for usar `ssh-agent`, ela pode definir uma passphrase depois com `ssh-keygen -p -f ~/.ssh/hive_servidor`.

O comando cria dois arquivos na pasta de entrega:

```text
~/hive-entregas/bruno-laptop/
  hive_servidor      ← chave PRIVADA: só para Bruno, nunca para o servidor
  hive_servidor.pub  ← chave PÚBLICA: vai para o authorized_keys de hive-bruno
```

Exiba a chave pública e anote a impressão digital; você vai colar a linha no servidor e comparar a impressão digital no passo 4:

```bash
cat "$HOME/hive-entregas/bruno-laptop/hive_servidor.pub"
ssh-keygen -lf "$HOME/hive-entregas/bruno-laptop/hive_servidor.pub"
```

A linha começa com `ssh-ed25519` e termina com o comentário `hive-bruno@bruno-laptop`. Não confunda os arquivos: somente o `.pub` é colado no servidor. Use um nome de arquivo diferente da sua própria chave administrativa, para não sobrescrevê-la.

## 4. No servidor: cadastrar a chave pública da pessoa

Use a chave **pública SSH** gerada no passo 3 (o conteúdo de `hive_servidor.pub`, uma linha que começa com `ssh-ed25519`) ou a que a pessoa enviou. Abra:

```bash
nano "/home/${HIVE_ADD_SSH}/.ssh/authorized_keys"
```

Cole a linha completa, mantendo quaisquer outras chaves já existentes. Salve com Ctrl+O, Enter; saia com Ctrl+X.

```bash
chown "$HIVE_ADD_SSH:$HIVE_ADD_SSH" "/home/${HIVE_ADD_SSH}/.ssh/authorized_keys"
chmod 600 "/home/${HIVE_ADD_SSH}/.ssh/authorized_keys"
ssh-keygen -lf "/home/${HIVE_ADD_SSH}/.ssh/authorized_keys"
/usr/sbin/sshd -t
```

A chave deve ser reconhecida, com a mesma impressão digital vista no passo 3, e `sshd -t` deve terminar sem erro. Não precisa reiniciar SSH ou Qdrant: a regra existente para o grupo `hive-tunnel` vale para a nova pessoa nas próximas conexões. Se o firewall restringe SSH por IP, permita também o IP dela na porta externa `20004` usada pelo script.

## 5. No servidor: gerar o JWT individual

```bash
python3 deploy/qdrant_admin.py --collection "$HIVE_ADD_COLLECTION" issue \
  --device-id "$HIVE_ADD_DEVICE" \
  --role writer \
  --hours 24 \
  --output "/srv/hive-private/${HIVE_ADD_DEVICE}.jwt"
```

Continue somente se retornar `"ok": true`. Guarde `token_id` e `expires_at` exibidos, sem divulgar o token. O arquivo no exemplo será `/srv/hive-private/bruno-laptop.jwt`.

O helper recusa sobrescrever um JWT existente. Se a pessoa já tinha sido cadastrada, você pode entregar o arquivo existente se ainda estiver válido. Para renovar, emita para um novo nome, como `bruno-laptop-renovacao-01.jwt`, e use esse caminho na cópia do passo 6. Emitir um novo token não revoga o anterior.

A validade começa na emissão: `--hours 24` dura 24 horas; o limite do helper é 168 horas. Não precisa criar uma CA nova: o arquivo compartilhado continua sendo `/srv/hive-private/ca.crt`.

## 6. No servidor: preparar CA e JWT para você copiar como administrador

O diretório `/srv/hive-private` é protegido. O bloco abaixo prepara uma **pasta privada dentro da sua conta administrativa**, permitindo a cópia com essa conta sem abrir acesso público aos tokens.

Confira primeiro se você informou a conta administrativa correta:

```bash
getent passwd "$HIVE_ADD_ADMIN"
```

Se não aparecer a conta, corrija `HIVE_ADD_ADMIN` antes de continuar. Depois:

```bash
HIVE_ADD_ADMIN_HOME="$(getent passwd "$HIVE_ADD_ADMIN" | cut -d: -f6)"
HIVE_ADD_ADMIN_GROUP="$(id -gn "$HIVE_ADD_ADMIN")"
HIVE_ADD_DELIVERY="${HIVE_ADD_ADMIN_HOME}/hive-entregas/${HIVE_ADD_DEVICE}"
install -d -m 700 -o "$HIVE_ADD_ADMIN" -g "$HIVE_ADD_ADMIN_GROUP" \
  "${HIVE_ADD_ADMIN_HOME}/hive-entregas" "$HIVE_ADD_DELIVERY"
install -m 600 -o "$HIVE_ADD_ADMIN" -g "$HIVE_ADD_ADMIN_GROUP" \
  /srv/hive-private/ca.crt "$HIVE_ADD_DELIVERY/ca.crt"
install -m 600 -o "$HIVE_ADD_ADMIN" -g "$HIVE_ADD_ADMIN_GROUP" \
  "/srv/hive-private/${HIVE_ADD_DEVICE}.jwt" "$HIVE_ADD_DELIVERY/writer.jwt"
ls -l "$HIVE_ADD_DELIVERY"
openssl x509 -in "$HIVE_ADD_DELIVERY/ca.crt" -noout -fingerprint -sha256
```

Agora há uma pasta pronta para entrega:

```text
/home/ubuntu/hive-entregas/bruno-laptop/
  ca.crt
  writer.jwt
```

Se sua conta administrativa for `root`, será `/root/hive-entregas/bruno-laptop/`. Cada pessoa tem uma pasta e um JWT próprios; todas recebem a mesma CA. A impressão digital deve ser conferida pelo participante.

## 7. Entregar os arquivos por canal privado

### 7.1. Administrador: buscar o pacote pessoal

**No seu computador de administrador, fora do SSH**, salve os arquivos na mesma pasta de entrega criada no passo 3, separada da sua instância. Assim, o token de Bruno não substitui a credencial de `moldret` no seu cliente.

O exemplo pressupõe que sua chave administrativa (`~/.ssh/hive_servidor`, diferente da chave gerada para Bruno em `~/hive-entregas/bruno-laptop/`) permite acesso como `ubuntu`. O script de túnel informa host `atlasgrid.site` e porta `20004`; em `scp`, a opção da porta é **`-P` maiúsculo**:

```bash
umask 077
mkdir -p "$HOME/hive-entregas/bruno-laptop"
chmod 700 "$HOME/hive-entregas/bruno-laptop"
scp -P 20004 -i "$HOME/.ssh/hive_servidor" \
  ubuntu@atlasgrid.site:/home/ubuntu/hive-entregas/bruno-laptop/ca.crt \
  ubuntu@atlasgrid.site:/home/ubuntu/hive-entregas/bruno-laptop/writer.jwt \
  "$HOME/hive-entregas/bruno-laptop/"
chmod 600 "$HOME/hive-entregas/bruno-laptop/ca.crt" \
  "$HOME/hive-entregas/bruno-laptop/writer.jwt"
openssl x509 -in "$HOME/hive-entregas/bruno-laptop/ca.crt" -noout -fingerprint -sha256
```

Para root, substitua as origens por `root@atlasgrid.site:/root/hive-entregas/bruno-laptop/ca.crt` e o equivalente para `writer.jwt`, mantendo `-P 20004`. Ajuste host/porta se os scripts da sua instalação já tiverem sido alterados.

### 7.2. Administrador: enviar somente ao destinatário

Envie a Bruno somente o `writer.jwt` dele, a `ca.crt` e a chave privada `hive_servidor` gerada no passo 3, por um canal individual com destinatário confirmado: por exemplo, compartilhamento restrito em um gerenciador de segredos ou conversa com criptografia de ponta a ponta. Se o canal permitir, use expiração do compartilhamento. Não envie o pacote em grupo ou como link público.

Informe também o nome que ele deve usar (`bruno`), a conta SSH (`hive-bruno`), o host/porta e a validade do JWT. Confirme a impressão digital da CA por um contato já confiável. A chave privada `hive_servidor` gerada para Bruno é dele: depois que ele confirmar o acesso, apague a sua cópia em `~/hive-entregas/bruno-laptop/`. Sua chave administrativa SSH e as chaves privadas do servidor não fazem parte da entrega.

Depois que Bruno confirmar o recebimento e validar o acesso, remova as cópias temporárias de entrega no servidor e no seu computador quando não forem mais necessárias. Excluir uma cópia do JWT não o revoga: se o token for exposto ou enviado à pessoa errada, revogue o `token_id` pelo helper e emita outro para o destinatário correto.

**Não use `hive-bruno` para essa cópia:** essa conta permite apenas túnel; a regra `MaxSessions 0` impede SCP/SFTP. Use sua conta administrativa para buscar os arquivos. O JWT não deve ficar num repositório Git nem numa pasta pública no servidor.

### 7.3. Participante: colocar os arquivos em hive_instance/config

**No computador de Bruno, na raiz da cópia dele de `hive_instance`**, crie a pasta de destino:

```bash
umask 077
mkdir -p config
chmod 700 config
```

Salve os anexos `writer.jwt` e `ca.crt` recebidos como `config/writer.jwt` e `config/ca.crt`. Se já os baixou em outra pasta, copie-os com `install`, substituindo os caminhos de origem abaixo:

```bash
install -m 600 /caminho/dos/arquivos/recebidos/writer.jwt config/writer.jwt
install -m 600 /caminho/dos/arquivos/recebidos/ca.crt config/ca.crt
openssl x509 -in config/ca.crt -noout -fingerprint -sha256
```

Compare a impressão digital com a informada pelo administrador. O JWT deve conter somente o token, sem aspas ou `QDRANT_API_KEY=`. Não é preciso abrir ou publicar seu conteúdo para conferir o recebimento.

Instale também a chave privada SSH recebida no caminho que `abrir-tunel.sh` espera:

```bash
install -d -m 700 "$HOME/.ssh"
install -m 600 /caminho/dos/arquivos/recebidos/hive_servidor "$HOME/.ssh/hive_servidor"
ssh-keygen -lf "$HOME/.ssh/hive_servidor"
```

A impressão digital deve ser a mesma informada pelo administrador. Sem a permissão `600`, o SSH recusa a chave.

```text
hive_instance/
  config/
    writer.jwt  ← credencial exclusiva desta pessoa
    ca.crt      ← CA pública da instalação
  scripts/
    inicializar.sh
    abrir-tunel.sh
  bin/
    hive-teste
    hive-mind-<os>-<arch>
```

O `.gitignore` já exclui `config/*`, exceto `.gitkeep`; não force a inclusão das credenciais no Git. O inicializador encontra os arquivos nesses caminhos. O JWT precisa ainda estar válido quando for usado; baixar/copiar novamente o mesmo arquivo não renova sua validade.

## 8. Inicialização e ajuste do túnel na cópia da nova pessoa

Entregue à pessoa ou ao inicializador:

| Campo | Exemplo |
| --- | --- |
| Servidor SSH | `atlasgrid.site`, porta `20004`, conforme o script analisado. |
| Conta de túnel | `hive-bruno`. |
| `HIVE_ID` | `hive-teste-4p`, igual ao primeiro participante. |
| `HIVE_COLLECTION` | `hive_teste_4p`. |
| `HIVE_DEVICE_ID` | `bruno-laptop`. |
| `HIVE_ROLE` | `writer`. |
| `HIVE_WRITER_APPROVAL_ID` | `teste-001-bruno-laptop`, exclusivo desse dispositivo e estável após o primeiro registro. |
| Arquivos | `ca.crt` e o `writer.jwt` individual de Bruno. |

O inicializador já fixa Hive, collection, papel writer, TLS e modelo; ele deriva dispositivo e approval ID do nome. O launcher lê `config/hive.toml` e `config/writer.jwt` da própria raiz de `hive_instance`.

**No computador de Bruno, na raiz de `hive_instance`:**

```bash
bash scripts/inicializar.sh bruno
```

Antes disso, confira se a cópia contém o executável `bin/hive-mind-<os>-<arch>` esperado pelo script. Na pasta local inspecionada nesta revisão havia apenas `bin/hive-teste`; faltavam os binários por plataforma. Sem o binário correspondente, o inicializador encerra antes de configurar as credenciais. Distribua uma compilação compatível com o SO/arquitetura da pessoa e a mesma revisão do Hive usada no ensaio.

O inicializador checa Python 3.11+, tenta preparar o modelo quando Ollama está instalado, gera a configuração local e configura os clientes MCP. Ele não emite JWT, não registra o writer no Qdrant e não abre o túnel automaticamente.

Edite `scripts/abrir-tunel.sh` nessa cópia. Troque apenas o usuário para a conta que você cadastrou e confira o caminho da chave privada **de Bruno**:

```bash
SSH_KEY="$HOME/.ssh/hive_servidor"
SSH_USER="hive-bruno"
SSH_HOST="atlasgrid.site"
SSH_PORT="20004"
```

Essas linhas são conteúdo do script. Exportar `SSH_USER` no terminal não resolve: a versão atual sobrescreve esse valor internamente com `ubuntu`. A chave pública correspondente à chave privada indicada deve estar no `authorized_keys` de `hive-bruno`.

Abra o túnel e deixe-o ativo:

```bash
bash scripts/abrir-tunel.sh
```

Em outro terminal, também na raiz de `hive_instance`, com Ollama disponível:

```bash
bin/hive-teste ingest
bin/hive-teste validate
bin/hive-teste status
```

A primeira ingestão varre `data/` da instância e cria o registro do writer; com a pasta vazia, não publica documentos. `validate` deve retornar `ok: true`; `status` deve mostrar `bruno-laptop` e a collection compartilhada. Essa primeira ingestão é necessária para um novo writer, embora os próximos passos impressos pelo inicializador atual comecem diretamente em `validate`.

Depois reconecte o MCP no cliente de IA. O certificado deve ter a mesma impressão digital conferida no servidor; o token precisa estar válido. O teste local de formato do JWT feito pelo inicializador não comprova assinatura/permissões, por isso a validação contra Qdrant continua necessária.

Por fim, confirme que a nova pessoa encontra uma nota sua e que você encontra uma nota publicada por ela. Para adicionar a próxima pessoa, repita este roteiro mudando os identificadores do passo 1.
