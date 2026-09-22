# Preparar um servidor Ubuntu 24.04 novo para o Hive Mind

Execute este arquivo **antes** do [guia do teste com quatro pessoas](GUIA_SERVIDOR_TESTE_4_PESSOAS.md). Ele instala os requisitos do servidor e verifica se você pode prosseguir para a configuração do Qdrant.

Todos os comandos, exceto a conexão SSH inicial, são executados **no servidor**. A preparação usa Docker Engine, Compose, Python, OpenSSL e OpenSSH. No modelo deste teste, Hive MCP e Ollama rodam nos computadores dos participantes; o servidor hospeda o Qdrant.

Os comandos abaixo foram revisados para Ubuntu **24.04 LTS**. Execute um bloco por vez; se algum comando falhar, resolva o erro antes de avançar. A instalação remota ainda não foi executada por este documento.

## 1. Entrar no servidor

**No terminal do seu computador:**

```bash
ssh -i ~/.ssh/hive_servidor USUARIO_ADMIN@IP_DO_SERVIDOR
```

Use o usuário fornecido pelo provedor, como `ubuntu` ou `root`, e o IP da instância. Se a chave tiver outro nome, ajuste o caminho. Na primeira conexão, confira a impressão digital do servidor pelo painel/console do provedor antes de aceitar.

**Já dentro do servidor**, confira o sistema:

```bash
cat /etc/os-release
dpkg --print-architecture
whoami
```

Espere Ubuntu, `VERSION_ID="24.04"`, e a arquitetura do servidor, normalmente `amd64` ou `arm64`.

Se você entrou como `ubuntu` ou outro administrador com sudo, abra uma sessão root:

```bash
sudo -i
```

Se já entrou como `root`, pule esse comando. **Os próximos comandos pressupõem essa sessão root**, por isso não repetem `sudo`.

## 2. Atualizar os pacotes e instalar as ferramentas básicas

```bash
apt-get update
apt-get upgrade -y
apt-get install -y \
  ca-certificates \
  curl \
  python3 \
  openssl \
  openssh-server \
  tar \
  gzip \
  iproute2 \
  nano
```

| Pacote | Uso no roteiro |
| --- | --- |
| `ca-certificates`, `curl` | Downloads HTTPS e verificação de certificados. |
| `python3` | Executar o helper administrativo de collections e credenciais. |
| `openssl` | Criar e verificar os certificados TLS do banco. |
| `openssh-server` | Acesso administrativo e túneis individuais. |
| `tar`, `gzip` | Extrair o pacote de implantação enviado do computador. |
| `iproute2` | Conferir portas com `ss`. |
| `nano` | Editar os arquivos de configuração. |

O helper do servidor usa a biblioteca padrão do Python: não precisa de `pip` nem de um ambiente virtual. Referência para SSH: [OpenSSH no Ubuntu](https://ubuntu.com/server/docs/how-to/security/openssh-server/).

Confira se a atualização pediu reinicialização:

```bash
if [ -f /var/run/reboot-required ]; then
  cat /var/run/reboot-required
fi
```

Se aparecer a indicação de reinicialização, execute:

```bash
reboot
```

A conexão SSH será encerrada. Aguarde a instância voltar, conecte novamente como no passo 1 e volte à sessão root antes de continuar. Se não houve indicação, siga diretamente.

## 3. Instalar Docker Engine e Compose pelo repositório oficial

Este passo considera um servidor novo, sem outra instalação Docker. Se sua imagem já veio com Docker e `docker compose` funcionando, confira as versões e siga para o passo 4; não misture instalações existentes. O procedimento e os pacotes estão na [documentação oficial do Docker para Ubuntu](https://docs.docker.com/engine/install/ubuntu/).

Instale a chave pública do repositório:

```bash
install -d -m 0755 /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg \
  -o /etc/apt/keyrings/docker.asc
chmod 0644 /etc/apt/keyrings/docker.asc
```

Cadastre o repositório. Este bloco detecta a arquitetura automaticamente; `noble` é o Ubuntu 24.04:

```bash
HIVE_BOOTSTRAP_ARCH="$(dpkg --print-architecture)"
cat > /etc/apt/sources.list.d/docker.sources <<EOF
Types: deb
URIs: https://download.docker.com/linux/ubuntu
Suites: noble
Components: stable
Architectures: ${HIVE_BOOTSTRAP_ARCH}
Signed-By: /etc/apt/keyrings/docker.asc
EOF
```

Instale os pacotes:

```bash
apt-get update
apt-get install -y \
  docker-ce \
  docker-ce-cli \
  containerd.io \
  docker-buildx-plugin \
  docker-compose-plugin
systemctl enable --now docker
```

O comando usado pelo guia é **`docker compose`**, com espaço. Nos comandos administrativos fora da sessão root, use `sudo docker ...`. As contas dos participantes criadas posteriormente serão contas de túnel SSH, sem acesso administrativo ao Docker.

## 4. Verificar Docker e baixar a imagem usada no teste

```bash
systemctl is-active docker
systemctl is-enabled docker
docker version
docker compose version
docker run --rm hello-world
```

Espere `active`, `enabled`, informações de Client/Server do Docker, a versão do Compose e a mensagem de sucesso do container de teste.

Baixe a versão do Qdrant selecionada pelo arquivo de implantação deste projeto:

```bash
docker pull qdrant/qdrant:v1.18.3
docker image inspect qdrant/qdrant:v1.18.3 --format '{{.Id}}'
```

Esse download apenas prepara a imagem. O Qdrant será iniciado pelo Compose no guia seguinte, depois de configurar segredos e certificados.

## 5. Conferir SSH e sincronização de horário

```bash
/usr/sbin/sshd -t
ss -lnt
timedatectl status
timedatectl show -p NTPSynchronized --value
```

`sshd -t` deve terminar sem erro. `ss` permite conferir os listeners da instância, incluindo SSH. O Ubuntu pode gerenciar SSH por ativação de socket; neste passo basta validar a configuração e manter seu acesso existente. As restrições de túnel serão configuradas no guia do teste.

O último comando deve retornar `yes`, indicando sincronização do relógio. Isso importa para a expiração dos tokens. Se estiver desativada, tente habilitá-la:

```bash
timedatectl set-ntp true
```

Aguarde a sincronização e confira novamente. Se esse comando disser que NTP não é suportado e a imagem não tiver um serviço de sincronização instalado, instale Chrony:

```bash
apt-get install -y chrony
systemctl enable --now chrony
chronyc tracking
timedatectl status
```

Use esse bloco apenas se faltar o serviço; uma instalação já sincronizada não precisa trocar de provedor de horário. Referência: [sincronização de horário no Ubuntu](https://ubuntu.com/server/docs/explanation/networking/about-time-synchronisation/).

No firewall do provedor, mantenha SSH acessível a você e, depois, às origens autorizadas dos participantes. O teste usa túneis SSH: as portas Qdrant `6333`/`6334` e a porta Ollama `11434` não precisam de entrada pública. O Compose do projeto publicará o banco somente em loopback.

## 6. Verificação final e continuação

```bash
python3 --version
openssl version
docker compose version
systemctl is-active docker
df -h /
free -h
```

O Ubuntu 24.04 normalmente fornece Python 3.12 e OpenSSL 3. O helper exige Python 3.10 ou superior. Verifique espaço livre em disco e memória disponíveis antes de adicionar seu acervo; o consumo dependerá da quantidade de documentos e vetores.

O servidor está preparado quando:

- Os comandos de instalação terminaram sem erro.
- Docker está ativo e inicia com o sistema.
- Compose responde e `hello-world` funcionou.
- A imagem `qdrant/qdrant:v1.18.3` foi baixada.
- Python e OpenSSL estão disponíveis.
- SSH continua acessível e o relógio está sincronizado.

**Agora continue no [guia principal, seção 4 — Enviar somente a implantação ao servidor](GUIA_SERVIDOR_TESTE_4_PESSOAS.md#4-enviar-somente-a-implantação-ao-servidor).** A seção 3 daquele guia já foi atendida por esta preparação.

A seção 4 começa no **seu computador**, na raiz do repositório: ela cria e envia o pacote por `scp`. Depois você retorna ao servidor para extrair os arquivos. Execute a extração com a conta administrativa que recebeu o pacote, antes de `sudo -i`, como indicado ali, para que `$HOME` aponte para a pasta correta.

No restante do guia você criará certificados, iniciará o Qdrant, provisionará as collections, emitirá quatro tokens e conectará os clientes. Nenhum documento foi ingerido apenas por executar esta preparação.
