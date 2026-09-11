# Backup, recuperação e incidentes

O script `scripts/hive_backup.py` cria um backup consistente das collections de dados e controle, desde que o operador interrompa todas as escritas durante a operação. O script não para serviços nem comprova sozinho a exclusividade do writer. Não execute contra produção sem janela e responsável definidos no [perfil operacional](deployment-profile.md).

## Preparação

1. Instale Python 3.10+ e restic a partir de uma distribuição verificada. Crie um repositório de backup privado, separado do armazenamento do Qdrant.
2. Provisione credenciais exclusivas de backup; não reutilize o token MCP. O acesso a snapshots precisa ser aprovado e testado pelo administrador Qdrant. A chave administrativa nunca vai para o processo Hive.
3. Monte um volume criptografado para staging. Crie nele um diretório absoluto `0700`, fora da pasta de documentos. Crie outro diretório privado `0700` para auditoria.
4. Injete os segredos pelo secret manager. Não passe tokens/senhas como argumentos, não habilite `set -x`, não publique relatórios de restic com paths reais.

Ambiente obrigatório:

| Variável | Conteúdo |
| --- | --- |
| `HIVE_ID`, `HIVE_COLLECTION` | Hive e collection de dados; controle é `<collection>__control`. |
| `HIVE_BACKUP_STAGING_DIR` | Diretório absoluto privado em disco criptografado. |
| `HIVE_AUDIT_DIR` | Diretório absoluto privado fora dos documentos. |
| `RESTIC_REPOSITORY` | Repositório restic aprovado. |
| `RESTIC_PASSWORD_FILE` ou `RESTIC_PASSWORD` | Senha pelo secret manager; arquivo, se usado, deve ser privado. |
| `HIVE_BACKUP_QDRANT_URL` | Endpoint REST, normalmente porta `6333`; HTTPS obrigatório fora de loopback. |
| `QDRANT_API_KEY` | Credencial exclusiva de snapshots, somente na etapa de backup. |
| `QDRANT_TLS_CA_FILE` | CA privada, se necessária. |

Inicialize somente um repositório novo, após conferir o destino:

```bash
restic init
restic check
```

Não execute `init` em lugar de recuperar uma senha perdida. Guarde a chave de recuperação separadamente, com acesso de emergência controlado. O script usa a [criptografia e verificação do restic](https://restic.readthedocs.io/en/stable/040_backup.html); SHA-256 do manifesto detecta troca do par, mas não substitui autenticação criptográfica.

## Criar backup

1. Pare o writer, incluindo watchers, CLI, jobs e sessões MCP. Confirme que nenhum dispositivo alternativo está escrevendo. Se necessário, bloqueie temporariamente o acesso de escrita da identidade do writer.
2. Registre o início da janela e confirme explicitamente a parada:

```bash
export HIVE_BACKUP_WRITER_STOPPED=yes
python3 scripts/hive_backup.py backup
```

3. Só aceite `ok: true`. O script baixa snapshots de dados e controle, calcula hashes, grava um manifesto, salva o conjunto em uma única operação restic e executa `restic check --read-data`. Snapshot individual tem limite de 10 GiB; ajuste/teste a ferramenta antes de operar acima desse volume.
4. Guarde apenas ID do snapshot, horário, change e resultado no relatório sanitizado. O par incompleto nunca é enviado como backup concluído.
5. Retome o writer e execute `validate`/`status`. Em falha, investigue antes de retomar. Um backup pode ter sido criado no restic mesmo que a verificação posterior tenha falhado; não o considere validado automaticamente.

Os snapshots gerados no próprio Qdrant permanecem no servidor. Programe expiração desses arquivos e dos snapshots restic conforme a retenção aprovada; não confunda remoção do staging com expiração do backup. Antes de qualquer `restic forget --prune`, revise os IDs selecionados e faça uma simulação com `--dry-run`. A política depende dos dias definidos pelo operador, não de um número arbitrário no código.

O script remove seu staging temporário de backup após a operação. Em SSD, exclusão não garante apagamento físico: por isso staging e disco do Qdrant devem ser criptografados. Retenha logs `backup-*.json` por 30 dias com um job externo restrito a esse diretório; eles não fazem parte da rotação JSONL do binário.

## Restaurar em isolamento

Nunca misture dados de um instante com controle de outro. Não restaure somente uma collection e não importe snapshots diretamente sobre a produção para testar.

1. Provisione uma instância Qdrant isolada, com versão compatível com o snapshot, TLS/loopback e credenciais novas. Nenhum writer de produção pode alcançá-la.
2. Selecione o ID exato do backup. Injete credenciais restic e variáveis de Hive/staging/auditoria; acesso ao Qdrant original não é necessário:

```bash
python3 scripts/hive_backup.py restore --snapshot <id-do-snapshot>
```

3. A saída identifica um novo subdiretório `hive-restore-...` dentro do staging. O script verifica o repositório e os hashes/identidade do par. `qdrant_restore_drill: pending` é intencional: recuperar arquivos não prova que Qdrant consegue servi-los.
4. Com a identidade administrativa exclusiva do ensaio, use a API oficial [de snapshots do Qdrant](https://qdrant.tech/documentation/snapshots/) para upload: `POST /collections/<collection>/snapshots/upload?priority=snapshot` e a mesma operação para `<collection>__control`. Envie respectivamente `data.snapshot` e `control.snapshot` como multipart `snapshot`. Confirme o destino isolado antes de cada upload; não coloque credenciais na linha de comando.
5. Use um reader de ensaio limitado às duas collections e o mesmo fingerprint Ollama. Execute `validate`, `status`, buscas conhecidas e contexto por ativo. Confirme revisão ativa, escopo aprovado, exclusões, classificação e ausência de dados de outro Hive. Teste escrita negada e credencial revogada.
6. Confirme que nenhum staging ou revision antiga aparece em consultas e que os tombstones continuam efetivos. Meça idade do backup (RPO) e duração total da restauração (RTO).
7. Registre versão/digest do código, horários, IDs sanitizados, checks e resultados no relatório do gate `paired_restore`. Falha em qualquer etapa mantém o gate pendente/falho.

Após o ensaio, retenha ou remova o staging por política explícita. O script não apaga uma restauração incompleta para permitir investigação; esses arquivos continuam sensíveis.

## Incidente e retorno à operação

1. Isole a identidade comprometida pela VPN/firewall e revogue suas credenciais. Pare escritores antes de qualquer recuperação.
2. Preserve auditoria sanitizada e registre responsável/change/canal. Não copie documentos ou tokens para um ticket público.
3. Rotacione segredos expostos e verifique falha de autenticação da identidade antiga. Um JWT já emitido não ganha revogação instantânea apenas porque foi emitido outro: use o mecanismo efetivo da implantação (identidade de rede, validação de token ou rotação coordenada).
4. Restaure o par em isolamento e execute os checks acima. Decida o destino de produção somente com autorização do responsável e plano de rollback.
5. Para promover outro writer, revogue primeiro o antigo e atualize administrativamente o registro `writer_registration` na collection de controle conforme o [contrato](../spec/contracts/control-records.md). O comando de auditoria apenas registra o change, não transfere propriedade.
6. Libere um único writer, valide writer/readers, reconcilie a pasta canônica e registre o término. Não reaproveite automaticamente credenciais potencialmente comprometidas.
