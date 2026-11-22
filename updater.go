package main

// Atualização automática pelo GitHub Releases.
//
//  1. Consulta a última versão (ou a última versão de teste, no canal beta).
//  2. Baixa SHA256SUMS.txt e SHA256SUMS.txt.sig e confere a ASSINATURA ed25519
//     com a chave pública embutida abaixo: só quem tem a chave privada (secret
//     do GitHub Actions) consegue publicar uma atualização aceita.
//  3. Baixa o executável e confere o SHA-256 contra a lista assinada.
//  4. Troca o executável, fecha o painel, abre a versão nova e espera ela
//     responder. Se a versão nova não subir em 60 s, volta sozinho para a anterior.
//  5. A versão anterior fica guardada (.old) por 10 minutos de funcionamento;
//     se a nova cair repetidamente ao abrir, o próximo início volta para a anterior.
//
// Configuração, jobs e relatórios ficam na pasta de dados — não são tocados.

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
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

// updatePubKey: chave pública ed25519 (base64) que assina o SHA256SUMS.txt das versões.
// A chave privada correspondente fica SOMENTE no secret UPDATE_SIGNING_KEY do GitHub.
var updatePubKey = "tFvXzuTAWnn1NuAZRGvwTY1MOm/aYLrQPyhCKBqBV/8="

const (
	updHealthWait  = 60 * time.Second // quanto a versão anterior espera a nova responder
	updConfirmAfter = 10 * time.Minute // a nova precisa ficar no ar esse tempo para apagar a anterior
	updMaxStarts   = 3                 // inícios sem confirmação antes de voltar para a anterior
)

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
	Beta       bool      `json:"beta"`
	Notes      string    `json:"notes"`
	URL        string    `json:"url"`
	Published  time.Time `json:"published"`
	CheckedAt  time.Time `json:"checkedAt"`
	Error      string    `json:"error"`
	Installing bool      `json:"installing"`
	Stage      string    `json:"stage"`
	assetURL   string
	sumsURL    string
	sigURL     string
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

// parseVersion: "v2.11.1-beta2" → [2 11 1], "beta2"
func parseVersion(v string) ([]int, string) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	pre := ""
	if i := strings.IndexAny(v, "-+ "); i >= 0 { pre, v = strings.ToLower(v[i+1:]), v[:i] }
	var nums []int
	for _, p := range strings.Split(v, ".") {
		n, _ := strconv.Atoi(strings.TrimFunc(p, func(r rune) bool { return r < '0' || r > '9' }))
		nums = append(nums, n)
	}
	return nums, pre
}

// cmpPre compara "beta2" com "beta10" (número no fim conta como número). "" (versão final) é maior.
func cmpPre(a, b string) int {
	if a == b { return 0 }
	if a == "" { return 1 }
	if b == "" { return -1 }
	split := func(s string) (string, int) {
		i := len(s)
		for i > 0 && s[i-1] >= '0' && s[i-1] <= '9' { i-- }
		n, _ := strconv.Atoi(s[i:])
		return s[:i], n
	}
	ta, na := split(a)
	tb, nb := split(b)
	if ta != tb { if ta < tb { return -1 }; return 1 }
	if na != nb { if na < nb { return -1 }; return 1 }
	return 0
}

// versionNewer: "2.10" > "2.9" > "2.8.3"; "2.11" > "2.11-beta2" > "2.11-beta1" > "2.10.2"
func versionNewer(a, b string) bool {
	pa, xa := parseVersion(a)
	pb, xb := parseVersion(b)
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) { x = pa[i] }
		if i < len(pb) { y = pb[i] }
		if x != y { return x > y }
	}
	return cmpPre(xa, xb) > 0
}

// verifySums confere a assinatura ed25519 (base64) do conteúdo do SHA256SUMS.txt.
func verifySums(sums, sigB64 []byte, pubB64 string) bool {
	pub, err := base64.StdEncoding.DecodeString(pubB64)
	if err != nil || len(pub) != ed25519.PublicKeySize { return false }
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigB64)))
	if err != nil || len(sig) != ed25519.SignatureSize { return false }
	return ed25519.Verify(ed25519.PublicKey(pub), sums, sig)
}

// sumFor procura o SHA-256 de um arquivo no conteúdo do SHA256SUMS.txt.
func sumFor(sums []byte, name string) string {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name && len(f[0]) == 64 { return strings.ToLower(f[0]) }
	}
	return ""
}

var updClient = &http.Client{Timeout: 5 * time.Minute}

func ghGet(url string) (*http.Response, error) {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "RackMonitorByDon/"+AppVersion)
	req.Header.Set("Accept", "application/vnd.github+json")
	return updClient.Do(req)
}

// ghSmall baixa um arquivo pequeno (somas, assinatura).
func ghSmall(url string) ([]byte, error) {
	r, err := ghGet(url)
	if err != nil { return nil, err }
	defer r.Body.Close()
	if r.StatusCode != 200 { return nil, fmt.Errorf("HTTP %d", r.StatusCode) }
	return io.ReadAll(io.LimitReader(r.Body, 1<<20))
}

type ghRelease struct {
	Tag        string    `json:"tag_name"`
	Body       string    `json:"body"`
	HTMLURL    string    `json:"html_url"`
	Draft      bool      `json:"draft"`
	Prerelease bool      `json:"prerelease"`
	Published  time.Time `json:"published_at"`
	Assets     []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// CheckUpdate consulta o GitHub.
func (h *Hub) CheckUpdate() *UpdInfo {
	repo := h.updRepo()
	cfgMu.RLock(); beta := h.cfg.UpdateBeta; cfgMu.RUnlock()
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
	url := api + "/repos/" + repo + "/releases/latest"
	if beta { url = api + "/repos/" + repo + "/releases?per_page=20" }
	resp, err := ghGet(url)
	if err != nil { info.Error = "Sem acesso ao GitHub: " + err.Error(); return info }
	defer resp.Body.Close()
	if resp.StatusCode == 404 { info.Error = "Nenhuma versão publicada em github.com/" + repo + " (ou o repositório é privado)."; return info }
	if resp.StatusCode != 200 { info.Error = fmt.Sprintf("GitHub respondeu %d", resp.StatusCode); return info }
	var rel ghRelease
	if beta {
		var list []ghRelease
		if err := json.NewDecoder(resp.Body).Decode(&list); err != nil { info.Error = "Resposta inválida do GitHub"; return info }
		found := false
		for _, r := range list {
			if r.Draft { continue }
			if !found || versionNewer(r.Tag, rel.Tag) { rel, found = r, true }
		}
		if !found { info.Error = "Nenhuma versão publicada em github.com/" + repo + "."; return info }
	} else if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil { info.Error = "Resposta inválida do GitHub"; return info }
	info.Latest = strings.TrimPrefix(rel.Tag, "v")
	info.Beta = rel.Prerelease
	info.Notes, info.URL, info.Published = rel.Body, rel.HTMLURL, rel.Published
	for _, a := range rel.Assets {
		switch a.Name {
		case updAssetName(): info.assetURL = a.URL
		case "SHA256SUMS.txt": info.sumsURL = a.URL
		case "SHA256SUMS.txt.sig": info.sigURL = a.URL
		}
	}
	info.Available = versionNewer(info.Latest, AppVersion)
	if info.Available && info.assetURL == "" { info.Error = "A versão " + info.Latest + " não tem o arquivo " + updAssetName() + "."; info.Available = false }
	return info
}

func UpdSnapshot() UpdInfo { updMu.Lock(); defer updMu.Unlock(); return *updLast }

func setStage(s string) { updMu.Lock(); updLast.Stage = s; updMu.Unlock(); if globalHub != nil { globalHub.mu.Lock(); globalHub.dirty = true; globalHub.mu.Unlock() } }

func exePath() (string, error) {
	exe, err := os.Executable()
	if err != nil { return "", err }
	if r, err := filepath.EvalSymlinks(exe); err == nil { exe = r }
	return exe, nil
}

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
	exe, err := exePath()
	if err != nil { return fail(err) }

	// lista de somas assinada
	setStage("conferindo assinatura…")
	if info.sumsURL == "" || info.sigURL == "" { return fail(fmt.Errorf("versão %s sem SHA256SUMS.txt assinado — atualização recusada por segurança", info.Latest)) }
	sums, err := ghSmall(info.sumsURL)
	if err != nil { return fail(fmt.Errorf("SHA256SUMS.txt: %v", err)) }
	sig, err := ghSmall(info.sigURL)
	if err != nil { return fail(fmt.Errorf("SHA256SUMS.txt.sig: %v", err)) }
	if !verifySums(sums, sig, updatePubKey) { return fail(fmt.Errorf("a assinatura da versão %s NÃO confere — atualização recusada por segurança", info.Latest)) }
	want := sumFor(sums, updAssetName())
	if want == "" { return fail(fmt.Errorf("SHA256SUMS.txt da versão %s não lista %s", info.Latest, updAssetName())) }

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
	// o .exe em uso pode ser renomeado (Windows), mas não sobrescrito
	if err := os.Rename(exe, old); err != nil { os.Remove(tmp); return fail(fmt.Errorf("não foi possível substituir o programa: %v", err)) }
	if err := os.Rename(tmp, exe); err != nil { os.Rename(old, exe); return fail(fmt.Errorf("não foi possível substituir o programa: %v", err)) }
	_ = os.WriteFile(exe+".pending", []byte(AppVersion+"\n0\n"), 0o644)
	setRegVersion(info.Latest)
	h.LogO(origin, "", "RACK MONITOR", "ATUALIZADO", "warn", fmt.Sprintf("Atualizado de V%s para V%s. Reiniciando o monitor…", AppVersion, info.Latest), nil)
	setStage("reiniciando…")
	go h.handoff(exe, info.Latest)
	return nil
}

// handoff: libera a porta, abre a versão nova e espera ela responder.
// Se não responder, desfaz a troca e reabre a versão anterior.
func (h *Hub) handoff(exe, newVer string) {
	time.Sleep(800 * time.Millisecond) // a resposta da API chega ao navegador
	cfgMu.RLock(); port := h.cfg.HTTPPort; cfgMu.RUnlock()
	// fecha o painel e o arquivo do job: a versão nova assume os dois
	if httpSrv != nil { _ = httpSrv.Close() }
	h.mu.Lock(); if h.jobLog != nil { h.jobLog.close(); h.jobLog = nil }; h.mu.Unlock()

	restart := func(path string, env ...string) (*exec.Cmd, error) {
		args := []string{}
		for _, a := range os.Args[1:] { if a != "-no-browser" && a != "--no-browser" { args = append(args, a) } }
		args = append(args, "-no-browser") // o navegador já aberto reconecta sozinho
		cmd := exec.Command(path, args...)
		cmd.Env = append(append(os.Environ(), "RACKMON_UPDATED=1"), env...)
		hideWindow(cmd)
		return cmd, cmd.Start()
	}
	rollback := func(why string) {
		_ = os.Remove(exe + ".bad")
		_ = os.Rename(exe, exe+".bad")
		_ = os.Rename(exe+".old", exe)
		_ = os.Remove(exe + ".pending")
		setRegVersion(AppVersion)
		_, _ = restart(exe, "RACKMON_ROLLBACK="+newVer, "RACKMON_ROLLBACK_WHY="+why)
		os.Exit(0)
	}

	cmd, err := restart(exe)
	if err != nil { rollback("não abriu: " + err.Error()) }
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	deadline := time.Now().Add(updHealthWait)
	for time.Now().Before(deadline) {
		select {
		case err := <-exited:
			rollback(fmt.Sprintf("a versão nova fechou ao abrir (%v)", err))
		case <-time.After(time.Second):
		}
		if v := runningVersion(port); v == newVer {
			os.Exit(0) // a nova está no ar: pronto
		}
	}
	_ = cmd.Process.Kill()
	rollback("a versão nova não respondeu em 60 s")
}

// runningVersion: versão do monitor que responde na porta ("" se nenhum).
func runningVersion(port int) string {
	cl := http.Client{Timeout: 2 * time.Second}
	r, err := cl.Get(fmt.Sprintf("http://127.0.0.1:%d/api/whoami", port))
	if err != nil { return "" }
	defer r.Body.Close()
	var w struct{ Version string `json:"version"` }
	_ = json.NewDecoder(r.Body).Decode(&w)
	return w.Version
}

func setRegVersion(v string) {
	if runtime.GOOS != "windows" { return }
	c := exec.Command("reg", "add", `HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall\RackMonitorByDon`, "/v", "DisplayVersion", "/t", "REG_SZ", "/d", v, "/f")
	hideWindow(c)
	_ = c.Run()
}

// afterUpdateStartup: chamado no início do main, antes de abrir a porta.
func afterUpdateStartup(port int) {
	if exe, err := exePath(); err == nil {
		os.Remove(exe + ".bad")
		pending := exe + ".pending"
		if b, err := os.ReadFile(pending); err == nil {
			// versão recém-instalada ainda não confirmada: conta os inícios
			lines := strings.Fields(string(b))
			prev, starts := "", 0
			if len(lines) > 0 { prev = lines[0] }
			if len(lines) > 1 { starts, _ = strconv.Atoi(lines[1]) }
			starts++
			if starts > updMaxStarts {
				if _, err := os.Stat(exe + ".old"); err == nil && os.Getenv("RACKMON_UPDATED") != "1" {
					// caiu várias vezes sem completar 10 minutos: volta para a anterior
					_ = os.Rename(exe, exe+".bad")
					if os.Rename(exe+".old", exe) == nil {
						os.Remove(pending)
						setRegVersion(prev)
						cmd := exec.Command(exe, os.Args[1:]...)
						cmd.Env = append(os.Environ(), "RACKMON_ROLLBACK="+AppVersion, "RACKMON_ROLLBACK_WHY=a versão nova fechou várias vezes logo depois de abrir")
						hideWindow(cmd)
						if cmd.Start() == nil { os.Exit(0) }
					}
					_ = os.Rename(exe+".bad", exe) // não conseguiu: segue com a nova
				}
				os.Remove(pending)
			} else {
				_ = os.WriteFile(pending, []byte(fmt.Sprintf("%s\n%d\n", prev, starts)), 0o644)
			}
		} else {
			os.Remove(exe + ".old")
		}
	}
	if os.Getenv("RACKMON_UPDATED") != "1" { return }
	os.Unsetenv("RACKMON_UPDATED")
	// espera a versão anterior liberar a porta
	for i := 0; i < 40 && alreadyRunning(port); i++ { time.Sleep(500 * time.Millisecond) }
	time.Sleep(300 * time.Millisecond)
}

// confirmUpdate: depois de 10 minutos no ar, a versão nova está aprovada.
func confirmUpdate() {
	exe, err := exePath()
	if err != nil { return }
	if _, err := os.Stat(exe + ".pending"); err != nil { return }
	time.Sleep(updConfirmAfter)
	os.Remove(exe + ".pending")
	os.Remove(exe + ".old")
}

// noteRollback: chamado no main quando esta versão foi reaberta por uma volta automática.
func (h *Hub) noteRollback() {
	bad := os.Getenv("RACKMON_ROLLBACK")
	if bad == "" { return }
	why := os.Getenv("RACKMON_ROLLBACK_WHY")
	os.Unsetenv("RACKMON_ROLLBACK"); os.Unsetenv("RACKMON_ROLLBACK_WHY")
	cfgMu.Lock(); h.cfg.UpdateBadVersion = bad; _ = saveConfig(h.paths, h.cfg); cfgMu.Unlock()
	h.Log("", "RACK MONITOR", "ATUALIZAÇÃO DESFEITA", "crit", fmt.Sprintf("A V%s foi desfeita (%s). Voltou para a V%s. A instalação automática dessa versão fica suspensa.", bad, why, AppVersion), nil)
}

// nearSchedule: perto (±30 min) de algum item da agenda de hoje?
func nearSchedule(items []ScheduleItem, now time.Time) bool {
	wd := int(now.Weekday())
	if wd == 0 { wd = 7 }
	for _, it := range items {
		if it.Days != "" && !strings.Contains(it.Days, strconv.Itoa(wd)) { continue }
		t, err := time.ParseInLocation("15:04", it.Time, now.Location())
		if err != nil { continue }
		at := time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, now.Location())
		if d := now.Sub(at); d > -30*time.Minute && d < 30*time.Minute { return true }
	}
	return false
}

// autoInstallBlocked: motivo para NÃO instalar sozinho agora ("" = pode).
func (h *Hub) autoInstallBlocked(latest string) string {
	cfgMu.RLock()
	bad := h.cfg.UpdateBadVersion
	sched := append([]ScheduleItem(nil), h.cfg.Schedule...)
	cfgMu.RUnlock()
	if bad != "" && bad == latest { return "versão desfeita anteriormente" }
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, s := range h.states { if s.Deck != nil && s.Deck.Recording() { return "gravador em REC" } }
	if h.recArmed { return "vigilância de gravação armada" }
	if nearSchedule(sched, time.Now()) { return "perto de um horário da agenda" }
	return ""
}

// updateLoop: confere ao abrir e a cada 6 h. Instala sozinho só se configurado
// e apenas quando nada estiver gravando, a vigilância estiver desarmada e
// não houver item da agenda perto.
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
				h.mu.Unlock()
				if !notified { h.Log("", "RACK MONITOR", "ATUALIZAÇÃO DISPONÍVEL", "info", "Nova versão V"+info.Latest+" disponível. CONFIGURAÇÃO → SISTEMA → ATUALIZAR.", nil) }
				if auto {
					if why := h.autoInstallBlocked(info.Latest); why == "" {
						_ = h.InstallUpdate("automático")
					} else if why != "versão desfeita anteriormente" {
						time.Sleep(15 * time.Minute) // tenta de novo daqui a pouco
						continue
					}
				}
			}
		}
		time.Sleep(6 * time.Hour)
	}
}
