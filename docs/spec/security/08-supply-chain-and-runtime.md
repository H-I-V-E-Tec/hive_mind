# Segurança 08 — Cadeia de dependências e runtime

## Objetivo

Reduzir o risco de dependências, builds, imagens ou permissões comprometidas.

## Requisitos

- Dependências Go permanecem fixadas em `go.mod`/`go.sum` e são revisadas por vulnerabilidades conhecidas.
- CI executa testes, análise estática, verificação de vulnerabilidades e scanner de segredos.
- Releases são reproduzíveis quando possível, incluem checksums e registram versão/commit de origem.
- Imagem de container usa base mínima, usuário não root, filesystem somente leitura quando possível e sem ferramentas desnecessárias.
- Processo local usa privilégio mínimo, permissões restritas e limites de CPU, memória, arquivos e conexões adequados.
- Nenhum download ou atualização automática ocorre durante a execução normal.

## Aceite e testes

- Pipeline bloqueia teste, análise ou vulnerabilidade crítica falhando.
- Artefato publicado corresponde ao checksum e commit documentados.
- Verificação de container confirma usuário não root e ausência de segredo incorporado.
