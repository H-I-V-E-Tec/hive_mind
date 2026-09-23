# Documentação do Hive Mind

- [Visão estratégica](vision/hive-mind-strategic-vision.md): tese do produto, arquitetura organizacional e hipóteses a validar.
- [Specs](spec/README.md): etapas e contratos de implementação da v0.1.
- [Contratos](spec/contracts/): schemas e registros persistidos pelas specs.
- [Segurança](spec/security/README.md): ameaças, controles e gates obrigatórios.
- [Decisões](decisions/README.md): decisões arquiteturais que orientam o produto.
- [Perfil operacional](operations/deployment-profile.md): RPO, RTO, retenções e dispositivos da implantação.
- [Como usar](../README.md): instalação, configuração, exemplos MCP e comandos.
- [Configurar Qdrant e Codex localmente](guias/CONFIGURAR_QDRANT_E_CODEX.md): laboratório local com writer, reader e integração MCP.
- [Conversão e ingestão](operations/semantic-ingestion.md): formatos, envelope comum, chunks, proveniência e exemplos copiáveis.
- [Servidor e múltiplos usuários](operations/server-multiuser-guide.md): Qdrant privado, TLS, tokens individuais e MCP local em cada cliente.
- [Preparar Ubuntu 24.04 recém-criado](guias/PREPARAR_SERVIDOR_UBUNTU_24_04.md): instalar e conferir os requisitos antes de configurar o servidor do teste.
- [Passo a passo: Ubuntu 24.04 e quatro pessoas](guias/GUIA_SERVIDOR_TESTE_4_PESSOAS.md): pacote mínimo do servidor, clientes com dados novos e teste de conversão/ingestão via MCP.
- [Adicionar uma pessoa no servidor existente](guias/ADICIONAR_PESSOA_NO_SERVIDOR.md): conta SSH, JWT individual e entrega privada de credenciais para `hive_instance`.
- [Atualizar o Hive Mind e os clientes](guias/ATUALIZAR_HIVE_MIND.md): ferramenta MCP nova, release, atualização do `hive_instance`, deploy do Qdrant e cadastro de pessoa.
- [Catálogo de alvos v1.0.0](vision/target-catalog-v1.md): proposta da ferramenta MCP para descobrir e priorizar alvos a partir de escopo, notas e recon.
- [Publicar e fazer deploy manualmente](guias/PUBLICAR_E_DEPLOY_MANUAL.md): disparo da `v0.2.0`, atualização do `hive_instance`, deploy separado do Qdrant e próximas tags.
- [Estado da implementação](operations/implementation-status.md): progresso e validações externas pendentes.
- [Acesso ao Qdrant](operations/qdrant-access.md): provisionamento, papéis, admissão e revogação.
- [Backup e recuperação](operations/backup-recovery.md): backup pareado, ensaio isolado e incidentes.
- [Verificação operacional opcional](operations/security-release.md): ensaios e vínculo dos relatórios com o código, quando desejados.
- [Entrega e deploy](operations/delivery-and-deployment.md): releases assinadas, atualização do cliente e promoção/rollback do Qdrant.
- [Ensaio de quatro pessoas](operations/four-person-trial.md): protocolo de duas semanas para continuidade, multi-writer e economia de contexto.

O Hive Mind é um produto dedicado à memória compartilhada de recon autorizado. O código herdado neste repositório serve apenas como referência técnica: não há compromisso de compatibilidade com sua configuração, CLI, payloads ou ferramentas MCP.
