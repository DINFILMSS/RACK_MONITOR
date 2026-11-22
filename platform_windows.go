//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"rackdon/internal/winlnk"
)

var (
	user32      = syscall.NewLazyDLL("user32.dll")
	pMessageBox = user32.NewProc("MessageBoxW")
)

const (
	mbOK        = 0x0
	mbYesNo     = 0x4
	mbIconInfo  = 0x40
	mbIconWarn  = 0x30
	mbIconError = 0x10
	idYes       = 6
)

func msgBox(text string, flags uintptr) int {
	t, _ := syscall.UTF16PtrFromString(text)
	c, _ := syscall.UTF16PtrFromString(appTitle)
	r, _, _ := pMessageBox.Call(0, uintptr(unsafe.Pointer(t)), uintptr(unsafe.Pointer(c)), flags|0x00010000) // MB_SETFOREGROUND
	return int(r)
}

func showError(text string) { msgBox(text, mbOK|mbIconError) }
func askYesNo(text string) bool { return msgBox(text, mbYesNo|mbIconInfo) == idYes }
func showInfo(text string) { msgBox(text, mbOK|mbIconInfo) }

// powershell roda um comando sem janela (o script só usa aspas simples).
func powershell(script string) error {
	c := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	hideWindow(c)
	out, err := c.CombinedOutput()
	if err != nil { return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out))) }
	return nil
}

func psq(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func makeShortcut(lnk, target, args, workdir, desc string) error {
	if err := winlnk.Create(lnk, target, args, workdir, desc); err == nil { return nil }
	return powershell(fmt.Sprintf(`$s=(New-Object -ComObject WScript.Shell).CreateShortcut(%s);$s.TargetPath=%s;$s.Arguments=%s;$s.WorkingDirectory=%s;$s.Description=%s;$s.IconLocation=%s;$s.Save()`,
		psq(lnk), psq(target), psq(args), psq(workdir), psq(desc), psq(target+",0")))
}

func startupLnk() string {
	return filepath.Join(os.Getenv("APPDATA"), `Microsoft\Windows\Start Menu\Programs\Startup`, appTitle+".lnk")
}

func autostartEnabled() (bool, error) { _, err := os.Stat(startupLnk()); return err == nil, nil }

func setAutostart(on bool) error {
	if !on { err := os.Remove(startupLnk()); if os.IsNotExist(err) { return nil }; return err }
	exe, _ := os.Executable()
	return makeShortcut(startupLnk(), exe, "", filepath.Dir(exe), appTitle)
}

const uninstKey = `HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall\RackMonitorByDon`

func startMenuDir() string { return filepath.Join(os.Getenv("APPDATA"), `Microsoft\Windows\Start Menu\Programs`, appTitle) }
func desktopLnk() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Desktop", appTitle+".lnk")
}

// runUninstall: chamado por "RackMonitorByDon.exe -uninstall" (Adicionar/Remover Programas).
func runUninstall() {
	if !askYesNo("Remover o RACK MONITOR BY DON deste computador?\n\nSeus jobs, relatórios e configuração ficam guardados em\n" + os.Getenv("LOCALAPPDATA") + `\DON_RACK_MONITOR` + "\n(apague essa pasta se quiser remover tudo).") { return }
	exe, _ := os.Executable()
	dir := filepath.Dir(exe)
	stopRunning(8787)
	stopRunning(8788)
	_ = os.RemoveAll(startMenuDir())
	_ = os.Remove(desktopLnk())
	_ = os.Remove(startupLnk())
	c := exec.Command("reg", "delete", uninstKey, "/f")
	hideWindow(c)
	_ = c.Run()
	showInfo("RACK MONITOR BY DON removido.")
	// apaga a pasta do programa depois que este processo terminar
	d := exec.Command("cmd", "/c", "ping 127.0.0.1 -n 3 > nul & rmdir /s /q \""+dir+"\"")
	hideWindow(d)
	_ = d.Start()
}
