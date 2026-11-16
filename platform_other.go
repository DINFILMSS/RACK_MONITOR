//go:build !windows && !darwin

package main

import (
	"fmt"
	"os"
)

func showError(text string) { fmt.Fprintln(os.Stderr, "ERRO:", text) }
func showInfo(text string)  { fmt.Println(text) }
func autostartEnabled() (bool, error) { return false, fmt.Errorf("não suportado neste sistema") }
func setAutostart(on bool) error { return fmt.Errorf("não suportado neste sistema") }
func runUninstall() {}
