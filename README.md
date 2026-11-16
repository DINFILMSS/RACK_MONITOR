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
Quando sai uma, aparece **⬆ NOVA VERSÃO** no topo do painel — um clique e ele baixa, confere a assinatura SHA-256 e reinicia sozinho.
Configuração, jobs e relatórios ficam na pasta de dados e não são tocados.

Opções em **CONFIGURAÇÃO → SISTEMA → ATUALIZAÇÕES** (procurar automaticamente, instalar sozinho quando nenhum gravador estiver em REC).

> O repositório precisa ser **público** para os programas instalados verem as versões sem senha.

## Publicar uma versão nova

1. Altere o código e atualize `docs/NOVIDADES.md` (vira o texto da release).
2. Crie a tag da versão — por exemplo `v2.11`:
   - no site: **Releases → Draft a new release → Choose a tag → `v2.11` → Publish release**, ou
   - no git: `git tag v2.11 && git push origin v2.11`
3. O GitHub Actions (aba **Actions**) compila Windows + Mac em ~3 minutos e anexa os arquivos à release.
4. Os monitores instalados avisam a versão nova.

A versão do programa vem da tag (não precisa editar números no código).

## Compilar na mão

Precisa de Go 1.24+.

```bash
go build .                      # roda no sistema atual
go run . -demo                  # modo demonstração com equipamentos simulados
GOOS=windows go build -ldflags "-H windowsgui" -o RackMonitorByDon.exe .
```

## Estrutura

- `*.go` — monitor (drivers ATEM/HyperDeck/VideoHub/SNMP…, alertas, relatórios, API e atualização)
- `web/` — painel (HTML/JS/CSS embutidos no executável)
- `installer/` — instalador do Windows (por usuário, sem administrador)
- `tools/macbundle.py` — monta os apps do Mac (binário universal)
- `.github/workflows/publicar.yml` — compila e publica a cada tag
