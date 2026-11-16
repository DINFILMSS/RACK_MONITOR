//go:build darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func osaQuote(s string) string { return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"` }

func showError(text string) {
	_ = exec.Command("osascript", "-e", "display dialog "+osaQuote(text)+" with title "+osaQuote(appTitle)+" buttons {\"OK\"} default button 1 with icon stop").Run()
}
func showInfo(text string) {
	_ = exec.Command("osascript", "-e", "display dialog "+osaQuote(text)+" with title "+osaQuote(appTitle)+" buttons {\"OK\"} default button 1").Run()
}

func agentPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", "com.bydon.rackmonitor.plist")
}

func autostartEnabled() (bool, error) { _, err := os.Stat(agentPath()); return err == nil, nil }

func setAutostart(on bool) error {
	if !on { err := os.Remove(agentPath()); if os.IsNotExist(err) { return nil }; return err }
	exe, _ := os.Executable()
	_ = os.MkdirAll(filepath.Dir(agentPath()), 0o755)
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>com.bydon.rackmonitor</string>
<key>ProgramArguments</key><array><string>%s</string><string>-no-browser</string></array>
<key>RunAtLoad</key><true/>
</dict></plist>
`, exe)
	return os.WriteFile(agentPath(), []byte(plist), 0o644)
}

func runUninstall() { showInfo("No Mac, para remover: arraste o RACK MONITOR BY DON de Aplicativos para o Lixo.") }
