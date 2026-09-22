# Inventário local — versão 1

Estado: implementado. Comando de diagnóstico local, sem conexão a Qdrant/Ollama, sem carregar configuração Hive e sem publicar ou remover documentos.

## Uso

```bash
hive-mind inventory /caminho/da/copia-do-hive-data
hive-mind inventory /caminho/da/copia-do-hive-data --program=demo --details
hive-mind inventory /caminho/da/copia-do-hive-data --hash-max-bytes=104857600
```

O diretório é obrigatório. O filtro seleciona `programs/<id>/` dentro dele; para analisar um programa, passe a raiz do acervo, não a pasta do programa. Sem filtro, arquivos fora dessa convenção também entram nas contagens, mas não são agrupados como duplicatas.

Flags de serviços, `--config` e opções desconhecidas são rejeitadas. O comando depende das permissões locais do sistema operacional sobre o diretório escolhido, não de credenciais writer/reader de Qdrant. Não está exposto no MCP.

## Seleção e leitura

- Conta arquivos regulares de qualquer extensão. A extensão é apenas uma categoria observada, em minúsculas; string vazia significa sem extensão. Não valida MIME, conteúdo JSON, PDF ou compatibilidade com a ingestão.
- Exclui entradas ocultas e aplica os padrões dos `.gitignore` da raiz e subdiretórios, usando o interpretador já existente no projeto. Não usa configuração global do Git, `.git/info/exclude` nem promete implementar toda a sintaxe do Git.
- Carrega cada política por leitura delimitada a 1 MiB. Política ilegível, alterada, não regular ou por symlink impede percorrer aquele ramo e produz resultado parcial.
- Ignora symlinks e arquivos especiais; não segue symlink para selecionar um programa. A raiz explicitamente escolhida pelo operador pode ser resolvida pelo sistema operacional.
- Abre arquivos dentro de `os.Root`, verifica identidade, modo, tamanho e modificação antes/depois da leitura. Em Unix, abertura não bloqueante evita travamento caso um arquivo seja substituído por FIFO.
- Faz SHA-256 em streaming, com buffer de 64 KiB. O limite por arquivo é 50 MiB por padrão, configurável de 1 byte até 1 GiB.
- Arquivos acima do limite são contados por tamanho, mas não têm conteúdo lido nem participam dos grupos. Isso reduz cobertura, sem tornar a execução uma falha.
- Falhas de leitura ficam registradas e os demais arquivos continuam. Cancelamento interrompe a varredura; caminhos e erros brutos não são exibidos por padrão.

O inventário não é um snapshot atômico do filesystem. Para uma linha de base reproduzível, usar cópia privada estável; alteração concorrente que preserve artificialmente identidade/tamanho/mtime não é uma garantia coberta. Limites de diretório, bind mounts e arquivos especiais são os do filesystem e de `os.Root`; implantações suportadas devem usar armazenamento local regular. Memória para listagem/grupos cresce com o número de entradas.

## Relatório JSON

- `schema_version: 1`, `ok`, `exit_code`, `scan_complete` e limite efetivo `hash_max_bytes`.
- `files`, `bytes` e `empty_files`: arquivos regulares observados, seus tamanhos e quantidade vazia.
- `hashed_files` e `hashed_bytes`: cobertura de leitura verificada; falhas não contribuem.
- `over_limit_files`: arquivos apenas medidos por excederem o limite.
- `skipped_entries`: entradas excluídas (um diretório inteiro conta uma entrada, não todos os descendentes).
- `failed_entries` e `issues`: falhas sanitizadas de entradas/políticas/varredura.
- `formats`: contagem e bytes por extensão, ordenados por extensão.
- `duplicate_groups`: candidatos a cópia integral com SHA-256 igual dentro do mesmo programa.
- `duplicate_files`: soma de `files - 1` para os grupos de repetição.
- `repeated_bytes`: soma de `(files - 1) * bytes_per_file` desses grupos.

Os grupos são ordenados deterministicamente por programa e hash interno. O hash não é exportado. `--details` acrescenta paths relativos dos grupos e das falhas; nunca texto dos documentos, destino absoluto de links ou erros internos. Os identificadores de programa e extensões são metadados presentes mesmo no resumo; o relatório continua sendo informação privada.

Arquivos vazios têm o mesmo hash e podem formar grupos com zero bytes repetidos. Hardlinks contam como caminhos distintos; `repeated_bytes` é repetição lógica, não espaço de disco recuperável. O inventário não interpreta classificação, autoria ou permissões do conteúdo e não autoriza fusão/remoção. SHA-256 seleciona candidatos; uma deduplicação com efeitos persistentes deverá verificar representação canônica e domínio completo de acesso.

Saídas: `0` para varredura concluída, `2` para opções inválidas, `10` para raiz indisponível, `15` para falha parcial/cancelamento. `scan_complete: false` não significa ausência de dados. Não inclui timestamps, de modo que a mesma cópia, opções e plataforma produzam o mesmo relatório.
