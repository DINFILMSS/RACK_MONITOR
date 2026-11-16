package main

// RACK MONITOR BY DON — V2.0
// Um único executável, sem instalador, sem .NET, sem ATEM SDK.
// Abre o painel no navegador e serve a mesma tela para qualquer aparelho da rede.

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"time"
)

var AppVersion = "2.10" // na compilação do GitHub vem da tag (-X main.AppVersion=…)

//go:embed web
var webFS embed.FS

func main() {
	demo := flag.Bool("demo", false, "modo demonstração com equipamentos simulados (não toca na rede técnica)")
	port := flag.Int("port", 0, "porta HTTP do painel (padrão 8787)")
	data := flag.String("data", "", "pasta de dados alternativa")
	noBrowser := flag.Bool("no-browser", false, "não abrir o navegador ao iniciar")
	uninstall := flag.Bool("uninstall", false, "remover o programa (Windows)")
	flag.Parse()
	if *uninstall { runUninstall(); return }
	if os.Getenv("RACKMON_DEMO") == "1" { *demo = true } // app de demonstração no Mac

	fmt.Println("")
	fmt.Println("  ██  RACK MONITOR BY DON  ·  V" + AppVersion)
	fmt.Println("  ██  ATEM · HyperDeck · VideoHub · rede — protocolo nativo, sem SDK")
	fmt.Println("")

	paths := resolvePaths(*demo, *data)
	setupLogFile(paths)
	_, statErr := os.Stat(paths.Config)
	fresh := os.IsNotExist(statErr)
	cfg, notes := loadConfig(paths)
	// já existe um monitor aberto? só abre o painel dele, antes de tocar em job ou equipamentos
	chkPort := cfg.HTTPPort
	if *port > 0 { chkPort = *port } else if *demo { chkPort = 8788 }
	afterUpdateStartup(chkPort) // se veio de uma atualização, espera a versão anterior sair
	if alreadyRunning(chkPort) {
		if !*noBrowser { openBrowser(fmt.Sprintf("http://localhost:%d/", chkPort)) }
		return
	}
	if *demo {
		startDemo(cfg, fresh)
		if fresh { _ = saveConfig(paths, cfg) }
		if *port == 0 { *port = 8788 }
		fmt.Println("  MODO DEMONSTRAÇÃO — equipamentos simulados em 127.0.0.1")
	}
	if *port > 0 { cfg.HTTPPort = *port }
	for _, n := range notes { fmt.Println("  · " + n) }

	h := newHub(cfg, paths, *demo)
	globalHub = h
	h.StartupJob()
	for _, n := range notes { h.Log("", "RACK MONITOR", "MIGRAÇÃO", "info", n, nil) }
	h.Log("", "RACK MONITOR", "MONITOR INICIADO", "info", fmt.Sprintf("V%s em %s · %d equipamentos", AppVersion, cfg.StationName, len(cfg.Devices)), nil)
	h.applyDevices()
	go h.run()
	go h.companionLoop()
	go h.tslLoop()
	go h.updateLoop()

	static, _ := fs.Sub(webFS, "web")
	addr := net.JoinHostPort(cfg.Bind, strconv.Itoa(cfg.HTTPPort))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		// já existe um monitor aberto: só abre o painel dele
		if alreadyRunning(cfg.HTTPPort) {
			if !*noBrowser { openBrowser(fmt.Sprintf("http://localhost:%d/", cfg.HTTPPort)) }
			return
		}
		log.Printf("porta %d ocupada: %v", cfg.HTTPPort, err)
		showError(fmt.Sprintf("Não foi possível abrir a porta %d: outro programa está usando.\n\nFeche o programa que usa essa porta ou mude \"httpPort\" em:\n%s", cfg.HTTPPort, paths.Config))
		os.Exit(1)
	}
	url := fmt.Sprintf("http://localhost:%d/", cfg.HTTPPort)
	fmt.Println("  Painel nesta estação:   " + url)
	for _, ip := range localIPs() { fmt.Printf("  Tablets/celulares/TV:   http://%s:%d/\n", ip, cfg.HTTPPort) }
	fmt.Println("  Dados e jobs:           " + paths.Root)
	fmt.Print("\n  Mantenha esta janela aberta. Fechar a janela encerra o monitor.\n\n")
	if !*noBrowser { go func() { time.Sleep(600 * time.Millisecond); openBrowser(url) }() }
	mux := h.routes(static)
	h.systemRoutes(mux)
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(srv.Serve(ln))
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows": cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin": cmd = exec.Command("open", url)
	default: cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}
