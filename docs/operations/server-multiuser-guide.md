# Qdrant no servidor e MCP para várias pessoas

> **Guia canônico** de implantação multiusuário. A [variante Tailscale/Caddy](multi-machine-setup.md) é alternativa; para produção, siga este roteiro.

Este roteiro instala um Qdrant compartilhado em um servidor Linux e conecta clientes individuais por túnel SSH, com TLS e credenciais revogáveis. O MCP deste projeto usa **stdio local**: cada pessoa executa seu próprio binário Hive e Ollama; o banco é compartilhado. Não existe neste projeto uma URL HTTP/SSE de MCP para publicar no servidor.

**Modelo de acesso (leia antes de prometer isolamento):** "multiusuário" aqui significa **um Qdrant compartilhado + N clientes stdio locais**. A identidade é o par `HIVE_DEVICE_ID` + JWT do Qdrant — **não** há RBAC por pessoa na camada MCP, nem OAuth/sessão. Um token `rw` concede acesso à collection inteira; `program_id`/`classification`/propriedade por writer são filtros/regras da aplicação, não barreiras criptográficas. Writers são **colegas confiáveis, não tenants isolados**. Equipes que não podem ler os dados umas das outras precisam de pares de collections e tokens separados (ver seção 1).

```text
Cliente MCP → Hive local → Ollama local (embeddings)
                    └── gRPC/TLS → túnel SSH → Qdrant no servidor
```

O percurso é compatível com a [política de rede](../spec/security/03-network-and-cryptography.md): Qdrant continua publicado somente em loopback, e TLS também protege o tráfego que atravessa o túnel. Não é necessário liberar as portas do banco na internet.

## 1. Preparar servidor e identidades

No servidor, tenha Docker Engine com Compose, OpenSSH Server, Python 3.10+, OpenSSL 3, disco persistente e relógio sincronizado. Copie uma revisão aprovada deste projeto para `/opt/hive_mind`. Os comandos administrativos abaixo são executados em uma sessão de operador com privilégios de root, **no servidor**:

```bash
sudo -i
cd /opt/hive_mind
umask 077
python3 deploy/qdrant_admin.py init
```

O comando cria `/srv/hive-private` com modo `0700`, uma chave administrativa aleatória em `admin.key` e o arquivo `qdrant.env`, ambos `0600`. Ele não imprime segredos e recusa inicializar sobre arquivos existentes. Mantenha esses arquivos fora do repositório e do diretório de documentos. Quem controla root ou Docker controla o banco e os segredos; clientes comuns não devem pertencer ao grupo Docker do servidor.

Defina os identificadores da instalação; este guia usa:

| Campo | Exemplo | Compartilhamento |
| --- | --- | --- |
| `HIVE_ID` | `research-team` | Igual em todos os clientes deste Hive |
| `HIVE_COLLECTION` | `hive_mind_v01` | Igual em todos os clientes deste Hive |
| Writer | `ana-laptop` | Um identificador por dispositivo |
| Segundo writer | `bruno-laptop` | Token e aprovação próprios |
| Reader | `carla-laptop` | Token somente leitura próprio |

Pessoas com acesso às mesmas duas collections pertencem ao mesmo domínio de confiança. `program_id`, `classification` e propriedade dos documentos são filtros/regras do aplicativo; um token Qdrant permite acesso direto à collection inteira. Para equipes que não podem ler os dados umas das outras, use pares de collections separados, configurações separadas e tokens exclusivos. `HIVE_MAX_CLASSIFICATION` não é uma barreira criptográfica contra o portador do token.

## 2. Criar certificado TLS e iniciar o banco

Ainda como operador no servidor, gere uma CA privada e um certificado de servidor. A chave da CA fica fora do diretório montado no container. Estes comandos são para a primeira instalação; renovar certificados é uma operação separada.

```bash
install -d -m 700 /srv/hive-private/tls
openssl req -x509 -newkey rsa:3072 -nodes -sha256 -days 3650 \
  -keyout /srv/hive-private/ca.key -out /srv/hive-private/ca.crt \
  -subj '/CN=Hive Internal CA' \
  -addext 'basicConstraints=critical,CA:TRUE' \
  -addext 'keyUsage=critical,keyCertSign,cRLSign'
openssl req -new -newkey rsa:3072 -nodes -sha256 \
  -keyout /srv/hive-private/tls/server.key \
  -out /srv/hive-private/server.csr -subj '/CN=qdrant.hive.internal' \
  -addext 'subjectAltName=DNS:qdrant.hive.internal,IP:127.0.0.1' \
  -addext 'basicConstraints=critical,CA:FALSE' \
  -addext 'extendedKeyUsage=serverAuth'
openssl x509 -req -in /srv/hive-private/server.csr \
  -CA /srv/hive-private/ca.crt -CAkey /srv/hive-private/ca.key \
  -CAcreateserial -out /srv/hive-private/tls/server.crt \
  -days 90 -sha256 -copy_extensions copy
chmod 600 /srv/hive-private/ca.key /srv/hive-private/tls/server.key
openssl verify -CAfile /srv/hive-private/ca.crt /srv/hive-private/tls/server.crt
docker compose -p hive-server -f deploy/qdrant-server.compose.yml config --quiet
docker compose -p hive-server -f deploy/qdrant-server.compose.yml up -d
docker compose -p hive-server -f deploy/qdrant-server.compose.yml ps
ss -lnt
```

Confirme os listeners `127.0.0.1:6333` e `127.0.0.1:6334`. O arquivo [Compose do servidor](../../deploy/qdrant-server.compose.yml) fixa a versão do Qdrant, habilita JWT/TLS e persiste dados e snapshots em volumes Docker. Use sempre o mesmo nome de projeto `hive-server`, pois ele determina os nomes dos volumes. Não execute `down -v` para reiniciar ou atualizar.

No firewall do provedor e do host, autorize SSH somente das origens administrativas/clientes permitidas. Mantenha `6333`, `6334`, `6335` e `11434` sem entrada pública. Preserve a regra que permite sua sessão administrativa antes de alterar firewall. Confirme também a inacessibilidade do banco a partir de outra máquina.

Envie **somente `ca.crt`**, além do token individual de cada pessoa, pelo canal seguro da equipe. Confira a impressão digital da CA por um canal já confiável:

```bash
openssl x509 -in /srv/hive-private/ca.crt -noout -fingerprint -sha256
```

Monitore a validade do certificado e renove antes dos 90 dias, mantendo os mesmos SANs; após renovar, reinicie o serviço e valide os clientes gRPC. Para uma PKI existente, use sua CA/certificados equivalentes. [Configuração TLS oficial do Qdrant](https://qdrant.tech/documentation/security/#tls).

## 3. Preparar embeddings e provisionar collections

Na máquina do primeiro writer, compile a mesma revisão do projeto que será distribuída aos demais. São necessários Go 1.25+ e compilador C/C++ para CGO/tree-sitter:

```bash
go mod download
go build -trimpath -o bin/hive-mind .
./bin/hive-mind help
ollama pull nomic-embed-text
ollama list
curl -fsS http://127.0.0.1:11434/api/embed \
  -H 'Content-Type: application/json' \
  -d '{"model":"nomic-embed-text","input":"hive dimension probe"}' \
  | python3 -c 'import json,sys; print(len(json.load(sys.stdin)["embeddings"][0]))'
curl -fsS http://127.0.0.1:11434/api/tags \
  | python3 -c 'import json,sys; print(json.dumps([{k:m[k] for k in ("name","digest")} for m in json.load(sys.stdin)["models"]], indent=2))'
```

Ollama deve estar iniciado em `127.0.0.1:11434`. Anote a dimensão retornada e o digest completo do modelo. Todos os writers **e readers** precisam do mesmo nome, digest, dimensão e parâmetros de parser/chunker. Baixar novamente uma tag no futuro pode obter um digest diferente: preserve o artefato aprovado e execute `validate` em cada máquina. Não atualize o modelo de uma collection existente sem uma migração.

No servidor, informe a dimensão que acabou de medir; `768` abaixo é apenas um exemplo e deve ser substituído se a medição divergir:

```bash
cd /opt/hive_mind
python3 deploy/qdrant_admin.py provision --dimension 768
```

O helper [qdrant_admin.py](../../deploy/qdrant_admin.py) acessa a API REST administrativa local por TLS, cria collections ausentes e recusa schemas incompatíveis. Não remove nem substitui collections existentes. Ele provisiona:

| Collection | Vetores | Finalidade |
| --- | --- | --- |
| `hive_mind_v01` | Denso **sem nome**, dimensão medida, `Cosine`; esparso chamado `sparse` | Chunks e seus metadados |
| `hive_mind_v01__control` | `{}`: sem vetores | Manifesto, registro dos writers, revisões, heads e escopo |
| `hive_device_access` | `{}`: sem vetores | Registros administrativos para revogação de JWT |

O primeiro `ingest` cria os índices de payload, o manifesto de compatibilidade e o registro do writer. Cada writer adicional registra a própria aprovação em seu primeiro `ingest`. JWTs normais têm acesso somente às duas collections do Hive; ninguém recebe acesso direto à collection administrativa. [Permissões e JWT do Qdrant](https://qdrant.tech/documentation/security/#granular-access-api-keys).

## 4. Autorizar um túnel SSH por pessoa

No servidor, crie um usuário SSH por pessoa/dispositivo, sem sudo nem acesso a Docker. Exemplo para Ana; repita para os demais:

```bash
groupadd hive-tunnel
adduser --disabled-password --gecos '' hive-ana
usermod -aG hive-tunnel hive-ana
install -d -m 700 -o hive-ana -g hive-ana /home/hive-ana/.ssh
install -m 600 -o hive-ana -g hive-ana /dev/null /home/hive-ana/.ssh/authorized_keys
```

Execute `groupadd` somente uma vez e crie `authorized_keys` somente em contas novas. Edite o arquivo com a **chave pública** que Ana forneceu; a chave privada permanece com ela. Restrinja o grupo em `/etc/ssh/sshd_config.d/60-hive-tunnels.conf`:

```text
Match Group hive-tunnel
    AuthenticationMethods publickey
    PasswordAuthentication no
    AllowTcpForwarding local
    AllowStreamLocalForwarding no
    PermitOpen 127.0.0.1:6334
    AllowAgentForwarding no
    X11Forwarding no
    PermitTTY no
    PermitTunnel no
    GatewayPorts no
    MaxSessions 0
Match all
```

`MaxSessions 0` impede shell e SFTP, preservando encaminhamento de portas. Confira a política resultante da sua instalação; valide a configuração antes de recarregar SSH, preservando a sessão administrativa aberta:

```bash
sshd -t
systemctl reload ssh
```

Na máquina de Ana, abra e mantenha este processo ativo. Verifique previamente a chave de host do servidor; não desative a verificação de host:

```bash
ssh -N -T -o ExitOnForwardFailure=yes \
  -o ServerAliveInterval=30 -o ServerAliveCountMax=3 \
  -L 127.0.0.1:16334:127.0.0.1:6334 hive-ana@SEU_SERVIDOR
```

Assim, `127.0.0.1:16334` de Ana chega à porta gRPC `6334` do servidor. A porta REST `6333` permanece administrativa. Cada pessoa pode usar o mesmo número local porque são máquinas distintas. Se precisar manter a conexão automaticamente, configure o gerenciador de serviços da estação com esta mesma identidade e opções. [Opções de forwarding do OpenSSH](https://man.openbsd.org/sshd_config#AllowTcpForwarding).

## 5. Emitir credenciais individuais e configurar os clientes

No servidor, emita um JWT por dispositivo. A validade padrão é oito horas; ajuste ao período operacional e renove antes de expirar. O limite do helper é sete dias:

```bash
python3 deploy/qdrant_admin.py issue --device-id ana-laptop --role writer \
  --hours 8 --output /srv/hive-private/ana-laptop.jwt
python3 deploy/qdrant_admin.py issue --device-id bruno-laptop --role writer \
  --hours 8 --output /srv/hive-private/bruno-laptop.jwt
python3 deploy/qdrant_admin.py issue --device-id carla-laptop --role reader \
  --hours 8 --output /srv/hive-private/carla-laptop.jwt
```

Cada arquivo novo é criado como `0600`. A saída contém somente dispositivo, ID do token e validade. Transfira o JWT correto via secret manager/canal seguro individual. Não compartilhe o arquivo `admin.key`, `qdrant.env`, a chave TLS ou a chave da CA. Não use tokens em argumentos de comando nem execute `set -x`.

Na máquina de Ana, crie diretórios privados fora do repositório. Ajuste todos os caminhos ao usuário/SO local; o exemplo usa Linux:

```bash
install -d -m 700 /home/ana/.config/hive-mind
install -d -m 700 /home/ana/hive-audit
mkdir -p /home/ana/hive-data/programs/demo/notes
```

Salve a CA recebida em `/home/ana/.config/hive-mind/ca.crt`. Crie `/home/ana/.config/hive-mind/hive.toml` com um editor seguro, substituindo o placeholder pelo JWT individual de Ana, e aplique `chmod 600` antes do uso:

```toml
HIVE_ID = "research-team"
HIVE_DEVICE_ID = "ana-laptop"
HIVE_ROLE = "writer"
HIVE_WRITER_APPROVAL_ID = "change-ana-001"
HIVE_COLLECTION = "hive_mind_v01"
HIVE_DATA_DIR = "/home/ana/hive-data"
HIVE_AUDIT_DIR = "/home/ana/hive-audit"
QDRANT_URL = "https://127.0.0.1:16334"
QDRANT_TLS_CA_FILE = "/home/ana/.config/hive-mind/ca.crt"
QDRANT_TLS_SERVER_NAME = "qdrant.hive.internal"
QDRANT_API_KEY = "SUBSTITUIR_PELO_JWT_DE_ANA"
OLLAMA_URL = "http://127.0.0.1:11434"
EMBEDDING_MODEL = "nomic-embed-text"
HIVE_MAX_CLASSIFICATION = "internal"
```

O segredo também pode ser injetado como `QDRANT_API_KEY` pelo secret manager, omitindo a linha do TOML. O aplicativo não carrega `.env` automaticamente. Variáveis de ambiente prevalecem sobre TOML e flags prevalecem sobre ambos: elimine configurações antigas conflitantes. O arquivo com segredo não pode ficar dentro de `HIVE_DATA_DIR`.

Bruno usa seus próprios caminhos, JWT `writer`, `HIVE_DEVICE_ID` e `HIVE_WRITER_APPROVAL_ID`. Carla usa seu JWT `reader`, `HIVE_ROLE = "reader"`, sua auditoria e remove `HIVE_DATA_DIR`/`HIVE_WRITER_APPROVAL_ID`; mantém o mesmo modelo local. Repita a configuração do túnel para cada identidade.

## 6. Fazer a primeira ingestão e verificar compartilhamento

Na máquina de Ana, adicione um documento sintético a `programs/demo/notes/ana-smoke.md`:

```markdown
---
program_id: demo
document_type: note
classification: internal
source: smoke-test
collected_at: 2026-09-17T12:00:00Z
tags: [smoke]
asset_refs: [api.example.com]
---
# Teste de compartilhamento
O marcador verificável desta nota é farol-violeta-427.
```

Execute, na raiz do projeto local:

```bash
chmod 600 /home/ana/.config/hive-mind/hive.toml
./bin/hive-mind --config /home/ana/.config/hive-mind/hive.toml ingest
./bin/hive-mind --config /home/ana/.config/hive-mind/hive.toml validate
./bin/hive-mind --config /home/ana/.config/hive-mind/hive.toml status
./bin/hive-mind --config /home/ana/.config/hive-mind/hive.toml \
  search demo 'farol-violeta-427' --scope-status=unknown
```

O `ingest` deve publicar chunks e retornar `ok: true`; confira também contagens e motivos de arquivos ignorados. `validate` deve retornar `ok: true` e código zero. A aprovação de escopo é independente: este teste permanece `unknown` e não representa autorização sobre `example.com`.

Para um programa real, crie `scope.json` conforme o [contrato](../spec/contracts/scope-manifest.md) e revise a política antes de aprovar:

```bash
./bin/hive-mind --config /home/ana/.config/hive-mind/hive.toml scope approve demo
```

O comando mostra o resumo/hash e retorna `2` enquanto não houve confirmação. Somente após a revisão, repita com `--yes`; `scope approve` não substitui a ingestão das notas. Não copie uma política fictícia para ativos reais.

No segundo writer, execute primeiro `ingest` para criar seu próprio registro e depois `validate`. No reader, execute apenas `validate`, `status` e a mesma busca. Ela deve retornar o marcador sem copiar a pasta de Ana para Carla. O reader deve falhar com código `12` ao executar `ingest`; `validate` confirma que a credencial do reader é impedida de escrever nas duas collections.

Cada documento tem um writer proprietário. Use nomes distintos, como `notes/ana-...` e `notes/bruno-...`: se dois writers usam o mesmo caminho lógico, o segundo recebe `skipped`/`foreign_writer`. Não compartilhe `HIVE_DEVICE_ID` para contornar a propriedade.

## 7. Conectar os clientes MCP

Em um cliente que aceite configuração `mcpServers` com transporte stdio, use caminhos absolutos para o binário e o arquivo TOML. Ajuste o formato se o cliente adotar outro esquema:

```json
{
  "mcpServers": {
    "hive-mind": {
      "command": "/home/ana/hive_mind/bin/hive-mind",
      "args": ["--config", "/home/ana/.config/hive-mind/hive.toml"]
    }
  }
}
```

Não adicione `ingest` ou `serve` ao comando MCP: sem subcomando o executável inicia o transporte stdio. Mantenha túnel e Ollama ativos antes de iniciar o cliente. O JSON do cliente não precisa conter o token, pois o processo lê o TOML protegido.

Reconecte o MCP e confira `tools/list`, `hive_search` com `program_id: "demo"` e o marcador, depois `hive_get_context`. Repita em outra máquina e confirme a escrita negada ao reader. Cada cliente inicia uma sessão local; não existe porta MCP no servidor. Prefira um único processo writer por identidade/pasta para evitar watchers duplicados.

## 8. Renovar ou revogar uma pessoa

Para renovar, emita outro token para o mesmo dispositivo usando **outro arquivo de saída**, instale-o no cliente, reinicie a sessão MCP e execute `validate`. Depois revogue o ID antigo retornado na emissão:

```bash
python3 deploy/qdrant_admin.py revoke --device-id ana-laptop \
  --token-id ID_UUID_DO_TOKEN_ANTIGO
```

Emitir outro JWT não invalida o anterior. O helper usa a claim `value_exists`: remover seu registro de autorização invalida futuras requisições, sem rotacionar a chave administrativa nem afetar outras pessoas.

Para desligar uma pessoa, remova sua chave pública de `authorized_keys`, encerre as sessões/túneis SSH daquela conta e revogue todos os seus tokens:

```bash
python3 deploy/qdrant_admin.py revoke --device-id ana-laptop
```

Remover a chave SSH não encerra um túnel já aberto; confirme seu fechamento. Com uma conexão de teste controlada, o JWT antigo deve ser recusado pelo Qdrant e `validate` deve retornar `12`; com o túnel bloqueado, a falha pode ser `11` (conectividade), o que sozinho não comprova revogação do JWT. Confirme também que outro dispositivo continua funcionando. Registre o change com `audit record credential_revocation` em uma configuração operacional válida e guarde apenas IDs/horários/resultados. [Claim de revogação do Qdrant](https://qdrant.tech/documentation/security/#jwt-configuration).

O registro `writer_registration` e os documentos do writer revogado permanecem: não são credenciais. A transferência ou remoção desses documentos exige procedimento operacional explícito.

## 9. Persistência, backup e recuperação

Volumes Docker preservam os dados em reinícios, mas não substituem backup fora do servidor. Preencha responsável, RPO/RTO e retenção no [perfil operacional](deployment-profile.md). Mantenha cópias protegidas dos documentos canônicos de cada writer, da configuração, dos logs de auditoria e do artefato exato do modelo.

Use o [procedimento de backup e recuperação](backup-recovery.md) e `scripts/hive_backup.py` para salvar **dados e controle no mesmo conjunto** em um repositório restic criptografado. No servidor, configure o endpoint de backup como `https://127.0.0.1:6333`, a CA `/srv/hive-private/ca.crt` e uma credencial exclusiva de backup. Antes de executar:

```bash
export HIVE_BACKUP_WRITER_STOPPED=yes
python3 scripts/hive_backup.py backup
```

pare de fato **todos** os writers, incluindo sessões MCP, CLI e jobs, durante os dois snapshots. A variável confirma a condição, mas não para processos nem bloqueia escritas automaticamente. As demais variáveis de identidade, staging privado, auditoria e restic estão descritas no procedimento; o comando isolado acima não configura o repositório.

`hive_device_access` não faz parte do par salvo pelo script. Na restauração, recrie essa collection **vazia** e emita novos tokens aos dispositivos ainda autorizados; não restaure registros de autorização antigos, que poderiam reativar JWTs revogados. Proteja separadamente a chave administrativa/PKI no secret manager. Com uma nova chave administrativa, todos os tokens precisam ser reemitidos.

Faça um ensaio em instância isolada, com versão compatível: restaure as duas collections, recrie o controle de acesso e teste `validate`, marcador conhecido, escopo, exclusões e credencial revogada. Registre RPO/RTO medidos. [Snapshots oficiais do Qdrant](https://qdrant.tech/documentation/operations/snapshots/).

## Diagnóstico rápido

| Sintoma | Conferir |
| --- | --- |
| Código `10` | Campos obrigatórios, caminhos, TOML `0600`, auditoria e ambiente conflitante |
| Código `11` | Túnel ativo, porta local livre, Qdrant e Ollama acessíveis |
| Código `12` | JWT expirado/revogado, papel incorreto ou writer ainda sem registro |
| Código `13` | CA correta, validade e SAN `qdrant.hive.internal` do certificado |
| Código `14` | Mesmo modelo/digest, dimensão, versão e parâmetros de chunking |
| Código `15` no ingest | Falha parcial; examine motivos por documento antes de tentar novamente |
| Busca vazia | Programa, classificação, escopo, conclusão da ingestão e propriedade do arquivo |

Este roteiro descreve uma implantação reproduzível; o aceite de produção ainda exige executar os testes entre as máquinas reais, provar revogação e ensaiar restauração. A aprovação do projeto não deve ser inferida apenas de testes com serviços simulados.
