# RACK MONITOR BY DON

Monitor de rack de transmissão: **ATEM, HyperDeck, VideoHub, câmeras Blackmagic, Web Presenter, nobreak/switch (SNMP)** e qualquer equipamento IP.
Um único programa, sem .NET e **sem ATEM SDK** — fala o protocolo nativo dos equipamentos. Abre o painel no navegador e serve a mesma tela para qualquer computador, tablet ou celular da rede.

## Baixar

Vá em **[Releases](../../releases/latest)** e baixe:

| Sistema | Arquivo |
|---|---|
| Windows 10/11 | `RACK_MONITOR_BY_DON_Vx.y_Instalador_Windows.exe` |
| Mac (Intel e Apple Silicon) | `RACK_MONITOR_BY_DON_Vx.y_Mac.zip` |

Instruções completas: [docs/LEIA-ME.txt](docs/LEIA-ME.txt) e [docs/LEIA-ME MAC.txt](docs/LEIA-ME%20MAC.txt).

## Atualizações

Instalou uma vez, pronto: o programa procura versão nova neste repositório ao abrir e a cada 6 horas.
Quando sai uma, aparece **⬆ NOVA VERSÃO** no topo do painel — um clique e ele baixa, confere a **assinatura digital (ed25519)** e o SHA-256 e reinicia sozinho. Se a versão nova não subir, volta sozinho para a anterior.
Configuração, jobs e relatórios ficam na pasta de dados e não são tocados.

Opções em **CONFIGURAÇÃO → SISTEMA → ATUALIZAÇÕES** (procurar automaticamente, instalar sozinho quando nenhum gravador estiver em REC).

> O repositório precisa ser **público** para os programas instalados verem as versões sem senha.

## Publicar uma versão nova

**Uma vez só (obrigatório):** crie o secret `UPDATE_SIGNING_KEY` em *Settings → Secrets and variables → Actions* com a chave privada de assinatura. Sem ele a publicação para com erro — de propósito: os monitores recusam versões sem assinatura.
Opcional: `WINDOWS_CERT_PFX` (certificado .pfx em base64) e `WINDOWS_CERT_PASSWORD` assinam o programa e o instalador (sem o aviso azul do SmartScreen).

1. Altere o código e atualize `docs/NOVIDADES.md` (vira o texto da release).
2. Mude o número no arquivo **`VERSION`** (ex.: `2.11`) e envie para a branch `main`.
   *(Ou crie uma release `v2.11` no site: Releases → Draft a new release.)*
   Para testar antes, use uma versão com hífen (ex.: `2.11-beta1`): ela sai como *pre-release* e só chega aos monitores com "Receber versões de teste" ligado.
3. O GitHub Actions (aba **Actions**) roda os testes, compila Windows + Mac em ~3 minutos, assina e publica a release.
4. Os monitores instalados avisam a versão nova.

A versão do programa vem do `VERSION`/tag (não precisa editar números no código).

### Chave de assinatura

`go run ./tools/assinar -gerar` cria um par de chaves novo. A pública vai em `updatePubKey` (`updater.go`); a privada vai **só** no secret `UPDATE_SIGNING_KEY`. Trocar a chave exige publicar antes uma versão (assinada com a chave antiga) que já traga a pública nova. Guarde a privada também fora do GitHub (gerenciador de senhas): sem ela não dá para publicar atualizações automáticas.

### Proteja a branch `main`

Como um envio para `main` publica para todos os racks, ative em *Settings → Branches* a regra para `main` exigindo pull request e os testes passando.

## Compilar na mão

Precisa de Go 1.24+.

```bash
go build .                      # roda no sistema atual
go run . -demo                  # modo demonstração com equipamentos simulados
go test ./...                   # testes
GOOS=windows go build -ldflags "-H windowsgui" -o RackMonitorByDon.exe .
```

## Estrutura

- `*.go` — monitor (drivers ATEM/HyperDeck/VideoHub/SNMP…, alertas, relatórios, API e atualização)
- `web/` — painel (HTML/CSS embutidos no executável); `web/js/` — scripts do painel, carregados em ordem pelo `index.html`
- `security.go` — proteções da API (Host, sites de fora, senha de administrador, limite de tentativas)
- `internal/winlnk` — atalhos do Windows pela COM (sem PowerShell)
- `tools/assinar` — gera chaves e assina o `SHA256SUMS.txt` das versões
- `installer/` — instalador do Windows (por usuário, sem administrador)
- `tools/macbundle.py` — monta os apps do Mac (binário universal)
- `.github/workflows/publicar.yml` — compila e publica a cada tag
