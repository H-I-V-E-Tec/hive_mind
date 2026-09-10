# Segurança 10 — Verificação para release

## Objetivo

Definir gates objetivos para impedir uma release insegura da v0.1.

## Gates automatizados

- `go test ./...`, análise estática e verificação de vulnerabilidades passam.
- Testes negativos de autenticação, TLS, isolamento, path traversal, symlink, redaction e limites passam.
- Scanner não encontra segredo confirmado no código, fixtures, imagem ou histórico incluído na release.
- Dependências críticas sem correção ou mitigação bloqueiam a release.

## Gates operacionais

- Duas máquinas autorizadas passam em `validate`.
- Qdrant exige autenticação, apresenta certificado válido e não responde por interface pública.
- MCP e Ollama não possuem listener remoto.
- Criptografia em repouso está ativa nos dispositivos, volume e backups.
- Revogação de dispositivo, rotação de chave e restauração de backup foram ensaiadas.
- Logs permitem auditar ações sem conter segredos ou conteúdo sensível.

## Evidência e aprovação

Armazenar relatório sanitizado com versão, data, responsável, resultado de cada gate e exceções referenciando uma decisão. Qualquer gate obrigatório reprovado bloqueia a release; não existe aprovação silenciosa.
