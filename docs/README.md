# Documentação técnica do Hive Mind

Este diretório contém os documentos que acompanham o código e seus contratos. Guias de uso e operação estão no catálogo externo [`HIVE/guias`](../../guias/README.md), que exige publicação própria para funcionar fora do workspace compartilhado.

- [Visão estratégica](vision/hive-mind-strategic-vision.md): proposta de produto e arquitetura futura.
- [Specs e registro de execução](spec/README.md): requisitos versionados e aceite da implementação.
- [Contratos e schemas](spec/contracts/README.md): formatos e invariantes persistidos.
- [Segurança](spec/security/README.md): ameaças, controles e verificações.
- [Decisões](decisions/README.md): escolhas arquiteturais aceitas.
- [Plano atual](../PLANO_MELHORIA_E_LIMPEZA.md): limpeza do repositório, divisão cliente/servidor e roadmap comercial.
- [Hive Center](../../hive_center/README.md): projeto separado da API, com contrato inicial de login web e estado de membro.
- [README principal](../README.md): capacidades presentes e desenvolvimento.

O exemplo `operations/release-evidence.example.json` e o caminho de evidências sanitizadas permanecem aqui porque são consumidos pelo gate opcional do código. Não colocar credenciais ou relatórios operacionais privados neste diretório.
