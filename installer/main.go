//go:build windows

// Instalador do RACK MONITOR BY DON para Windows.
// Instala por usuário (sem administrador) em %LOCALAPPDATA%\Programs\RACK MONITOR BY DON,
// cria atalhos (Menu Iniciar, Área de Trabalho), entrada em "Adicionar ou remover
// programas" e, se você quiser, abre junto com o Windows. Mantém jobs e configuração.

package main

import (
	_ "embed"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"rackdon/internal/winlnk"
)

//go:embed payload/RackMonitorByDon.exe
var payload []byte

//go:embed payload/LEIA-ME.txt
var readme []byte

const (
	title   = "RACK MONITOR BY DON"
	exeName = "RackMonitorByDon.exe"
	regKey  = `HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall\RackMonitorByDon`
)

var version = "2.12" // na compilação do GitHub vem da tag (-X main.version=…)

var pMsg = syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW")

func box(text string, flags uintptr) int {
	t, _ := syscall.UTF16PtrFromString(text)
	c, _ := syscall.UTF16PtrFromString(title + " · Instalação")
	r, _, _ := pMsg.Call(0, uintptr(unsafe.Pointer(t)), uintptr(unsafe.Pointer(c)), flags|0x00010000)
	return int(r)
}
func ask(text string) bool { return box(text, 0x4|0x20) == 6 }
func info(text string)     { box(text, 0x40) }
func fail(text string)     { box(text, 0x10); os.Exit(1) }

func hidden(c *exec.Cmd) *exec.Cmd { c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}; return c }

func psq(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func shortcut(lnk, target, args, workdir, desc string) error {
	_ = os.MkdirAll(filepath.Dir(lnk), 0o755)
	if err := winlnk.Create(lnk, target, args, workdir, desc); err == nil { return nil }
	// reserva: PowerShell (só se a COM falhar)
	script := fmt.Sprintf(`$s=(New-Object -ComObject WScript.Shell).CreateShortcut(%s);$s.TargetPath=%s;$s.Arguments=%s;$s.WorkingDirectory=%s;$s.Description=%s;$s.IconLocation=%s;$s.Save()`,
		psq(lnk), psq(target), psq(args), psq(workdir), psq(desc), psq(target+",0"))
	out, err := hidden(exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)).CombinedOutput()
	if err != nil { return fmt.Errorf("%v %s", err, out) }
	return nil
}

func reg(name, typ, val string) { _ = hidden(exec.Command("reg", "add", regKey, "/v", name, "/t", typ, "/d", val, "/f")).Run() }

func running(port int) bool {
	cl := http.Client{Timeout: time.Second}
	r, err := cl.Get(fmt.Sprintf("http://127.0.0.1:%d/api/whoami", port))
	if err != nil { return false }
	r.Body.Close()
	return true
}

func stop(port int) {
	if !running(port) { return }
	cl := http.Client{Timeout: 2 * time.Second}
	if r, err := cl.Post(fmt.Sprintf("http://127.0.0.1:%d/api/system/shutdown", port), "application/json", nil); err == nil { r.Body.Close() }
	for i := 0; i < 24 && running(port); i++ { time.Sleep(250 * time.Millisecond) }
}

func main() {
	dir := filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", title)
	exe := filepath.Join(dir, exeName)
	_, existed := os.Stat(exe)
	verb := "Instalar"
	if existed == nil { verb = "Atualizar" }
	if !ask(fmt.Sprintf("%s o RACK MONITOR BY DON V%s?\n\nPasta: %s\nNão precisa de administrador.\n\nJobs, relatórios e configuração são mantidos.", verb, version, dir)) { return }

	stop(8787)
	stop(8788)
	if err := os.MkdirAll(dir, 0o755); err != nil { fail("Não foi possível criar a pasta:\n" + err.Error()) }
	var err error
	for i := 0; i < 10; i++ {
		if err = os.WriteFile(exe, payload, 0o755); err == nil { break }
		time.Sleep(500 * time.Millisecond) // arquivo ainda em uso
	}
	if err != nil { fail("Não foi possível gravar o programa (ele ainda está aberto?).\n\n" + err.Error()) }
	_ = os.WriteFile(filepath.Join(dir, "LEIA-ME.txt"), readme, 0o644)
	// restos de uma atualização automática anterior não valem para esta instalação
	for _, ext := range []string{".old", ".new", ".bad", ".pending"} { _ = os.Remove(exe + ext) }

	menu := filepath.Join(os.Getenv("APPDATA"), `Microsoft\Windows\Start Menu\Programs`, title)
	home, _ := os.UserHomeDir()
	errs := []string{}
	for _, s := range []struct{ lnk, args, desc string }{
		{filepath.Join(menu, title+".lnk"), "", "Abrir o painel do RACK MONITOR"},
		{filepath.Join(menu, "RACK MONITOR (demonstração).lnk"), "-demo", "Equipamentos simulados, sem tocar na rede técnica"},
		{filepath.Join(menu, "Desinstalar RACK MONITOR.lnk"), "-uninstall", "Remover o RACK MONITOR BY DON"},
		{filepath.Join(home, "Desktop", title+".lnk"), "", "Abrir o painel do RACK MONITOR"},
	} {
		if e := shortcut(s.lnk, exe, s.args, dir, s.desc); e != nil { errs = append(errs, filepath.Base(s.lnk)) }
	}
	reg("DisplayName", "REG_SZ", title)
	reg("DisplayVersion", "REG_SZ", version)
	reg("Publisher", "REG_SZ", "BY DON")
	reg("DisplayIcon", "REG_SZ", exe+",0")
	reg("InstallLocation", "REG_SZ", dir)
	reg("UninstallString", "REG_SZ", `"`+exe+`" -uninstall`)
	reg("NoModify", "REG_DWORD", "1")
	reg("NoRepair", "REG_DWORD", "1")
	reg("EstimatedSize", "REG_DWORD", fmt.Sprint(len(payload)/1024))

	startup := filepath.Join(os.Getenv("APPDATA"), `Microsoft\Windows\Start Menu\Programs\Startup`, title+".lnk")
	if ask("Abrir o RACK MONITOR automaticamente quando o Windows ligar?\n\n(Recomendado no computador do rack. Dá para mudar depois em CONFIGURAÇÃO → SISTEMA.)") {
		if e := shortcut(startup, exe, "", dir, title); e != nil { errs = append(errs, "inicialização automática") }
	} else { _ = os.Remove(startup) }

	msg := "Pronto! RACK MONITOR BY DON V" + version + " instalado.\n\n" +
		"• Abra pelo atalho da Área de Trabalho ou do Menu Iniciar.\n" +
		"• Ele roda sem janela: o painel abre no navegador.\n" +
		"• Para encerrar: CONFIGURAÇÃO → SISTEMA → ENCERRAR MONITOR.\n" +
		"• Na primeira vez o Windows pode perguntar sobre o Firewall: permita em REDES PRIVADAS para tablets e celulares acessarem.\n"
	if len(errs) > 0 { msg += "\nAtenção: não foi possível criar: " + strings.Join(errs, ", ") + ". O programa está em:\n" + exe + "\n" }
	if ask(msg + "\nAbrir o RACK MONITOR agora?") { _ = exec.Command(exe).Start() }
}
