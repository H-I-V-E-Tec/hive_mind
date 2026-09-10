# Segurança 10 — Verificação para release

## Objetivo

Definir gates objetivos para impedir uma release insegura da v0.1.

## Gates automatizados

- `go test ./...`, análise estática e verificação de vulnerabilidades passam.
- Testes negativos de papel/credencial, TLS, isolamento, revisão parcial, escopo forjado, path traversal, symlink, redaction e limites passam.
- Scanner não encontra segredo confirmado no código, fixtures, imagem ou histórico incluído na release.
- Dependências críticas sem correção ou mitigação bloqueiam a release.

## Gates operacionais

- Writer e reader passam em `validate`, apresentam fingerprint idêntico e permissões diferentes verificadas.
- Qdrant exige autenticação, apresenta certificado válido e não responde por interface pública.
- MCP e Ollama não possuem listener remoto.
- Criptografia em repouso está ativa nos dispositivos, volume e backups.
- Revogação individual, promoção segura de writer, rotação de credencial e restauração das duas collections foram ensaiadas.
- Logs permitem auditar ações sem conter segredos ou conteúdo sensível.

## Evidência e aprovação

Armazenar relatório sanitizado com versão, data, responsável, resultado de cada gate e exceções referenciando uma decisão. O relatório inclui uma matriz ameaça → controle → teste/evidência e o perfil de implantação com RPO, RTO e retenções numéricas. Qualquer gate obrigatório reprovado bloqueia a release; não existe aprovação silenciosa.
