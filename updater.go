package main

// Atualização automática pelo GitHub Releases.
// O programa consulta a última versão publicada no repositório, baixa o
// executável da plataforma, confere o SHA-256 (arquivo SHA256SUMS.txt da
// mesma versão), troca o próprio executável e reinicia sozinho.
// Configuração, jobs e relatórios ficam na pasta de dados — não são tocados.

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// UpdateRepo: "usuario/repositorio" no GitHub. Definido na compilação
// (-ldflags "-X main.UpdateRepo=usuario/repo") ou em CONFIGURAÇÃO.
var UpdateRepo = ""

func updAssetName() string {
	switch runtime.GOOS {
	case "windows": return "RackMonitorByDon_windows.exe"
	case "darwin": return "RackMonitorByDon_mac"
	}
	return "RackMonitorByDon_" + runtime.GOOS + "_" + runtime.GOARCH
}

type UpdInfo struct {
	Repo       string    `json:"repo"`
	Current    string    `json:"current"`
	Latest     string    `json:"latest"`
	Available  bool      `json:"available"`
	Notes      string    `json:"notes"`
	URL        string    `json:"url"`
	Published  time.Time `json:"published"`
	CheckedAt  time.Time `json:"checkedAt"`
	Error      string    `json:"error"`
	Installing bool      `json:"installing"`
	Stage      string    `json:"stage"`
	assetURL   string
	sumsURL    string
}

var (
	updMu   sync.Mutex
	updLast = &UpdInfo{Current: AppVersion}
)

func (h *Hub) updRepo() string {
	cfgMu.RLock(); r := strings.TrimSpace(h.cfg.UpdateRepo); cfgMu.RUnlock()
	if r == "" { r = UpdateRepo }
	r = strings.TrimPrefix(strings.TrimPrefix(r, "https://"), "github.com/")
	return strings.Trim(r, "/ ")
}

// versionNewer: "2.10" > "2.9" > "2.8.3"
func versionNewer(a, b string) bool {
	pa, pb := strings.Split(strings.TrimPrefix(a, "v"), "."), strings.Split(strings.TrimPrefix(b, "v"), ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) { x, _ = strconv.Atoi(strings.TrimFunc(pa[i], func(r rune) bool { return r < '0' || r > '9' })) }
		if i < len(pb) { y, _ = strconv.Atoi(strings.TrimFunc(pb[i], func(r rune) bool { return r < '0' || r > '9' })) }
		if x != y { return x > y }
	}
	return false
}

var updClient = &http.Client{Timeout: 5 * time.Minute}

func ghGet(url string) (*http.Response, error) {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "RackMonitorByDon/"+AppVersion)
	req.Header.Set("Accept", "application/vnd.github+json")
	return updClient.Do(req)
}

// CheckUpdate consulta o GitHub.
func (h *Hub) CheckUpdate() *UpdInfo {
	repo := h.updRepo()
	info := &UpdInfo{Repo: repo, Current: AppVersion, CheckedAt: time.Now()}
	defer func() {
		updMu.Lock()
		if updLast.Installing { info.Installing, info.Stage = true, updLast.Stage }
		updLast = info
		updMu.Unlock()
		h.mu.Lock(); h.dirty = true; h.mu.Unlock()
	}()
	if repo == "" || !strings.Contains(repo, "/") {
		info.Error = "Repositório de atualização não configurado (CONFIGURAÇÃO → SISTEMA → ATUALIZAÇÕES)."
		return info
	}
	api := "https://api.github.com"
	if v := os.Getenv("RACKMON_UPDATE_API"); v != "" { api = v } // só para testes
	resp, err := ghGet(api + "/repos/" + repo + "/releases/latest")
	if err != nil { info.Error = "Sem acesso ao GitHub: " + err.Error(); return info }
	defer resp.Body.Close()
	if resp.StatusCode == 404 { info.Error = "Nenhuma versão publicada em github.com/" + repo + " (ou o repositório é privado)."; return info }
	if resp.StatusCode != 200 { info.Error = fmt.Sprintf("GitHub respondeu %d", resp.StatusCode); return info }
	var rel struct {
		Tag       string    `json:"tag_name"`
		Body      string    `json:"body"`
		HTMLURL   string    `json:"html_url"`
		Published time.Time `json:"published_at"`
		Assets    []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil { info.Error = "Resposta inválida do GitHub"; return info }
	info.Latest = strings.TrimPrefix(rel.Tag, "v")
	info.Notes, info.URL, info.Published = rel.Body, rel.HTMLURL, rel.Published
	for _, a := range rel.Assets {
		if a.Name == updAssetName() { info.assetURL = a.URL }
		if a.Name == "SHA256SUMS.txt" { info.sumsURL = a.URL }
	}
	info.Available = versionNewer(info.Latest, AppVersion)
	if info.Available && info.assetURL == "" { info.Error = "A versão " + info.Latest + " não tem o arquivo " + updAssetName() + "."; info.Available = false }
	return info
}

func UpdSnapshot() UpdInfo { updMu.Lock(); defer updMu.Unlock(); return *updLast }

func setStage(s string) { updMu.Lock(); updLast.Stage = s; updMu.Unlock(); if globalHub != nil { globalHub.mu.Lock(); globalHub.dirty = true; globalHub.mu.Unlock() } }

// InstallUpdate baixa, confere, troca o executável e reinicia.
func (h *Hub) InstallUpdate(origin string) error {
	updMu.Lock()
	if updLast.Installing { updMu.Unlock(); return fmt.Errorf("atualização já em andamento") }
	updMu.Unlock()
	info := h.CheckUpdate()
	if !info.Available { if info.Error != "" { return fmt.Errorf("%s", info.Error) }; return fmt.Errorf("já está na versão mais recente (V%s)", AppVersion) }
	if h.demo { return fmt.Errorf("no modo demonstração a atualização fica desligada") }
	updMu.Lock(); updLast.Installing = true; updMu.Unlock()
	fail := func(err error) error {
		updMu.Lock(); updLast.Installing = false; updLast.Stage = ""; updLast.Error = err.Error(); updMu.Unlock()
		h.Log("", "RACK MONITOR", "ATUALIZAÇÃO FALHOU", "warn", err.Error(), nil)
		return err
	}
	exe, err := os.Executable()
	if err != nil { return fail(err) }
	if r, err := filepath.EvalSymlinks(exe); err == nil { exe = r }

	// soma SHA-256 esperada
	want := ""
	if info.sumsURL != "" {
		setStage("conferindo assinatura…")
		if r, err := ghGet(info.sumsURL); err == nil {
			sc := bufio.NewScanner(r.Body)
			for sc.Scan() {
				f := strings.Fields(sc.Text())
				if len(f) == 2 && strings.TrimPrefix(f[1], "*") == updAssetName() { want = strings.ToLower(f[0]) }
			}
			r.Body.Close()
		}
	}
	if want == "" { return fail(fmt.Errorf("versão %s sem SHA256SUMS.txt válido — atualização recusada por segurança", info.Latest)) }

	setStage("baixando V" + info.Latest + "…")
	resp, err := ghGet(info.assetURL)
	if err != nil { return fail(fmt.Errorf("download: %v", err)) }
	defer resp.Body.Close()
	if resp.StatusCode != 200 { return fail(fmt.Errorf("download: HTTP %d", resp.StatusCode)) }
	tmp := exe + ".new"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil { return fail(fmt.Errorf("sem permissão para gravar em %s: %v", filepath.Dir(exe), err)) }
	hs := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, hs), io.LimitReader(resp.Body, 200<<20))
	f.Close()
	if err != nil || n < 1<<20 { os.Remove(tmp); return fail(fmt.Errorf("download incompleto (%d bytes): %v", n, err)) }
	if got := hex.EncodeToString(hs.Sum(nil)); got != want { os.Remove(tmp); return fail(fmt.Errorf("arquivo baixado não confere (SHA-256) — atualização cancelada")) }

	setStage("instalando…")
	old := exe + ".old"
	os.Remove(old)
	if runtime.GOOS == "windows" {
		// no Windows o .exe em uso pode ser renomeado, mas não sobrescrito
		if err := os.Rename(exe, old); err != nil { os.Remove(tmp); return fail(fmt.Errorf("não foi possível substituir o programa: %v", err)) }
		if err := os.Rename(tmp, exe); err != nil { os.Rename(old, exe); return fail(fmt.Errorf("não foi possível substituir o programa: %v", err)) }
		_ = exec.Command("reg", "add", `HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall\RackMonitorByDon`, "/v", "DisplayVersion", "/t", "REG_SZ", "/d", info.Latest, "/f").Run()
	} else {
		if err := os.Rename(tmp, exe); err != nil { os.Remove(tmp); return fail(fmt.Errorf("não foi possível substituir o programa: %v", err)) }
	}
	h.LogO(origin, "", "RACK MONITOR", "ATUALIZADO", "warn", fmt.Sprintf("Atualizado de V%s para V%s. Reiniciando o monitor…", AppVersion, info.Latest), nil)
	setStage("reiniciando…")
	go func() {
		time.Sleep(800 * time.Millisecond)
		args := []string{}
		for _, a := range os.Args[1:] { if a != "-no-browser" && a != "--no-browser" { args = append(args, a) } }
		args = append(args, "-no-browser") // o navegador já aberto reconecta sozinho
		cmd := exec.Command(exe, args...)
		cmd.Env = append(os.Environ(), "RACKMON_UPDATED=1")
		hideWindow(cmd)
		if err := cmd.Start(); err != nil {
			// não conseguiu reiniciar: volta o executável antigo
			if runtime.GOOS == "windows" { os.Rename(exe, exe+".bad"); os.Rename(old, exe) }
			h.Log("", "RACK MONITOR", "ATUALIZAÇÃO FALHOU", "crit", "Não foi possível reiniciar: "+err.Error()+". Abra o programa de novo.", nil)
			return
		}
		os.Exit(0)
	}()
	return nil
}

// afterUpdateStartup: chamado no início do main.
func afterUpdateStartup(port int) {
	if exe, err := os.Executable(); err == nil {
		if r, err := filepath.EvalSymlinks(exe); err == nil { exe = r }
		defer os.Remove(exe + ".old")
		os.Remove(exe + ".bad")
	}
	if os.Getenv("RACKMON_UPDATED") != "1" { return }
	os.Unsetenv("RACKMON_UPDATED")
	// espera a versão anterior liberar a porta
	for i := 0; i < 40 && alreadyRunning(port); i++ { time.Sleep(500 * time.Millisecond) }
	time.Sleep(300 * time.Millisecond)
}

// updateLoop: confere ao abrir e a cada 6 h. Instala sozinho só se configurado
// e apenas quando nenhum gravador estiver em REC.
func (h *Hub) updateLoop() {
	time.Sleep(15 * time.Second)
	for {
		cfgMu.RLock(); off, auto := h.cfg.UpdateCheckOff, h.cfg.UpdateAuto; cfgMu.RUnlock()
		if !off && !h.demo {
			info := h.CheckUpdate()
			if info.Available {
				h.mu.Lock()
				notified := h.updNotified == info.Latest
				h.updNotified = info.Latest
				rec := false
				for _, s := range h.states { if s.Deck != nil && s.Deck.Recording() { rec = true } }
				h.mu.Unlock()
				if !notified { h.Log("", "RACK MONITOR", "ATUALIZAÇÃO DISPONÍVEL", "info", "Nova versão V"+info.Latest+" disponível. CONFIGURAÇÃO → SISTEMA → ATUALIZAR.", nil) }
				if auto && !rec { _ = h.InstallUpdate("automático") }
			}
		}
		time.Sleep(6 * time.Hour)
	}
}
