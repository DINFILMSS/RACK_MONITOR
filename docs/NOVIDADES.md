## Novidades da V2.11

- **Senha de administrador**: para alterar a configuração por outro aparelho da rede (tablet, celular, outro PC) é preciso digitar a senha definida no computador do rack. Sem senha definida, só o computador do rack altera a configuração. Todos continuam vendo o painel normalmente.
- **Formatar exige o PIN de comandos.**
- **Senhas protegidas**: a senha do e-mail e o token do Telegram não são mais enviados para o navegador.
- **Proteção contra sites maliciosos**: um site aberto no navegador do rack não consegue mais mandar comandos para o monitor (nem por DNS rebinding). Acesso por nome DNS próprio: inclua o nome em `allowedHosts` no config.json.
- **Atualização assinada**: o monitor só instala versões com assinatura digital válida.
- **Volta automática**: se a versão nova não abrir, ou cair nos primeiros minutos, o monitor volta sozinho para a anterior.
- **Instalar sozinho** espera também a vigilância desarmada e longe dos horários da agenda.
- **Canal beta** opcional para testar versões antes de liberar para todos.
- Atalhos do Windows criados sem PowerShell (menos alarmes de antivírus).
- Corrigido: opções de atualização e modelo de nome dos clipes não eram salvos.

## Da V2.10.2

- **Corrigido "102 invalid value" ao formatar**: o exFAT aceita no máximo **11 caracteres** no nome do cartão e o nome sugerido era maior. Agora o nome é ajustado sozinho (exFAT 11, HFS+ 27) e, se o gravador ainda recusar o nome, formata mantendo o nome atual do cartão.
- A lista de formatação mostra só os slots **com cartão montado** e marca qual está ATIVO.

## Da V2.10.1

- **Formatar cartão/SSD também no modo "Via Companion"**: quando o Companion ocupa a conexão do HyperDeck, a formatação agora vai pela **REST API** do HyperDeck (canal separado — não derruba o Companion). O botão **⌫ FORMATAR CARTÃO / SSD** aparece no card do gravador (com comandos liberados) e lista as mídias com nome, espaço livre e qual está ativa.
- Requer firmware do HyperDeck 8.4 ou mais novo.

## Da V2.10

- **Atualização automática pelo GitHub**: o monitor procura versão nova ao abrir e a cada 6 horas. Quando tem, aparece **⬆ NOVA VERSÃO** no topo — um clique e ele baixa, confere (SHA-256) e reinicia sozinho. Sem desinstalar, sem instalar de novo. Configuração, jobs e relatórios são mantidos.
- Opção de instalar sozinho (só quando nenhum gravador estiver em REC): CONFIGURAÇÃO → SISTEMA → ATUALIZAÇÕES.

### Da V2.9
- Painel mostra todas as M/E de cada ATEM.
- Nome automático nos clipes (REC pelo monitor).
- Inventário de cartões por job (imprimir/PDF/CSV).
- Alarme "ESQUECEU DE GRAVAR?".
