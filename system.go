package main

// Integração com o sistema: uma instância só, registro em arquivo (sem janela
// de comando), encerrar pelo painel, iniciar com o computador e desinstalar.

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

const appTitle = "RACK MONITOR BY DON"

// setupLogFile: grava o log em <dados>/rackmonitor.log (troca quando passa de 5 MB).
func setupLogFile(p Paths) string {
	_ = os.MkdirAll(p.Root, 0o755)
	lp := filepath.Join(p.Root, "rackmonitor.log")
	if fi, err := os.Stat(lp); err == nil && fi.Size() > 5<<20 { _ = os.Rename(lp, lp+".old") }
	f, err := os.OpenFile(lp, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil { return "" }
	log.SetOutput(io.MultiWriter(f, os.Stderr))
	log.Printf("==== %s V%s iniciando (%s/%s)", appTitle, AppVersion, runtime.GOOS, runtime.GOARCH)
	return lp
}

// alreadyRunning: se o painel já responde nesta porta, é outra cópia do monitor.
func alreadyRunning(port int) bool {
	cl := http.Client{Timeout: 1500 * time.Millisecond}
	r, err := cl.Get(fmt.Sprintf("http://127.0.0.1:%d/api/whoami", port))
	if err != nil { return false }
	r.Body.Close()
	return r.StatusCode == 200
}

func openPath(p string) {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "windows": c = exec.Command("explorer", p)
	case "darwin": c = exec.Command("open", p)
	default: c = exec.Command("xdg-open", p)
	}
	_ = c.Start()
}

func (h *Hub) systemRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/system", func(w http.ResponseWriter, r *http.Request) {
		on, err := autostartEnabled()
		writeJSON(w, map[string]any{"os": runtime.GOOS, "autostart": on, "autostartSupported": err == nil, "dataDir": h.paths.Root, "local": isLocal(r), "version": AppVersion, "defaultRepo": UpdateRepo})
	})
	m.HandleFunc("POST /api/system/autostart", func(w http.ResponseWriter, r *http.Request) {
		if !isLocal(r) { httpErr(w, 403, "Só no computador do rack."); return }
		var b struct{ On bool `json:"on"` }
		if !readJSON(w, r, &b) { return }
		if err := setAutostart(b.On); err != nil { httpErr(w, 500, err.Error()); return }
		h.LogO(originOf(r), "", "RACK MONITOR", "INICIAR COM O SISTEMA", "info", map[bool]string{true: "Ativado: o monitor abre sozinho ao ligar o computador.", false: "Desativado."}[b.On], nil)
		writeJSON(w, ok())
	})
	m.HandleFunc("POST /api/system/shutdown", func(w http.ResponseWriter, r *http.Request) {
		if !isLocal(r) { httpErr(w, 403, "Só no computador do rack."); return }
		h.LogO(originOf(r), "", "RACK MONITOR", "MONITOR ENCERRADO", "warn", "Encerrado pelo painel. O job continua ao abrir de novo.", nil)
		writeJSON(w, ok())
		go func() { time.Sleep(400 * time.Millisecond); os.Exit(0) }()
	})
	m.HandleFunc("GET /api/update", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("check") == "1" { h.CheckUpdate() }
		u := UpdSnapshot()
		u.Repo = h.updRepo()
		writeJSON(w, u)
	})
	m.HandleFunc("POST /api/update/install", func(w http.ResponseWriter, r *http.Request) {
		if !isLocal(r) { httpErr(w, 403, "Atualize no computador do rack."); return }
		if err := h.InstallUpdate(originOf(r)); err != nil { httpErr(w, 409, err.Error()); return }
		writeJSON(w, ok())
	})
	m.HandleFunc("POST /api/system/openfolder", func(w http.ResponseWriter, r *http.Request) {
		if !isLocal(r) { httpErr(w, 403, "Só no computador do rack."); return }
		openPath(h.paths.Root)
		writeJSON(w, ok())
	})
}

// stopRunning pede para uma cópia em execução encerrar (instalação/remoção).
func stopRunning(port int) {
	if !alreadyRunning(port) { return }
	cl := http.Client{Timeout: 2 * time.Second}
	if r, err := cl.Post(fmt.Sprintf("http://127.0.0.1:%d/api/system/shutdown", port), "application/json", nil); err == nil { r.Body.Close() }
	for i := 0; i < 20 && alreadyRunning(port); i++ { time.Sleep(250 * time.Millisecond) }
}
