# Documentação do Hive Mind

- [Visão estratégica](vision/hive-mind-strategic-vision.md): tese do produto, arquitetura organizacional e hipóteses a validar.
- [Specs](spec/README.md): etapas e contratos de implementação da v0.1.
- [Contratos](spec/contracts/): schemas e registros persistidos pelas specs.
- [Segurança](spec/security/README.md): ameaças, controles e gates obrigatórios.
- [Decisões](decisions/README.md): decisões arquiteturais que orientam o produto.
- [Perfil operacional](operations/deployment-profile.md): RPO, RTO, retenções e dispositivos da implantação.
- [Como usar](../README.md): instalação, configuração, exemplos MCP e comandos.
- [Estado da implementação](operations/implementation-status.md): progresso e validações externas pendentes.
- [Acesso ao Qdrant](operations/qdrant-access.md): provisionamento, papéis, admissão e revogação.
- [Backup e recuperação](operations/backup-recovery.md): backup pareado, ensaio isolado e incidentes.
- [Gate de release](operations/security-release.md): evidências obrigatórias e vínculo com o código.
- [Ensaio de quatro pessoas](operations/four-person-trial.md): protocolo de duas semanas para continuidade, multi-writer e economia de contexto.

O Hive Mind é um produto dedicado à memória compartilhada de recon autorizado. O código herdado neste repositório serve apenas como referência técnica: não há compromisso de compatibilidade com sua configuração, CLI, payloads ou ferramentas MCP.
