package main

// Painéis (ATEM Advanced Panel, Broadcast Panel 1/2 M/E, Camera Control Panel)
// e qualquer aparelho que só tenha IP: monitorados por PING (usa o ping do
// sistema — funciona sem rodar como administrador).
// A varredura também lê a tabela ARP para achar tudo que é Blackmagic pelo
// fabricante do MAC (7C:2E:0D), inclusive aparelhos sem porta de controle.

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	pingTimeRe = regexp.MustCompile(`(?i)(?:time|tempo|zeit|tiempo)\s*[=<]\s*([\d.,]+)\s*ms`)
	pingTTLRe  = regexp.MustCompile(`(?i)ttl[=:]`)
	macArpRe   = regexp.MustCompile(`\((\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3})\) at ([0-9a-fA-F:]+)`)
	arpRe      = regexp.MustCompile(`(?i)(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3})\D+?(([0-9a-f]{2}[:-]){5}[0-9a-f]{2})`)
)

func pingOnce(host string, timeout time.Duration) (bool, float64) {
	ms := int(timeout / time.Millisecond)
	var cmd *exec.Cmd
	ctx, cancel := context.WithTimeout(context.Background(), timeout+1500*time.Millisecond)
	defer cancel()
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "ping", "-n", "1", "-w", strconv.Itoa(ms), host)
	} else if runtime.GOOS == "darwin" {
		cmd = exec.CommandContext(ctx, "ping", "-c", "1", "-W", strconv.Itoa(ms), host) // macOS: -W em milissegundos
	} else {
		cmd = exec.CommandContext(ctx, "ping", "-c", "1", "-W", strconv.Itoa(max(1, ms/1000)), host)
	}
	hideWindow(cmd)
	out, _ := cmd.CombinedOutput()
	s := string(out)
	if !pingTTLRe.MatchString(s) { return false, 0 }
	lat := 0.5
	if m := pingTimeRe.FindStringSubmatch(s); m != nil {
		if f, err := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", "."), 64); err == nil { lat = f }
	}
	return true, lat
}

func startPing(h *Hub, d Device) func() {
	stop := make(chan struct{})
	go func() {
		fails := 0
		for {
			ok, lat := pingOnce(d.Host, time.Second)
			if ok {
				fails = 0
				h.Update(d.ID, func(s *DevState) {
					if s.Probe == nil { s.Probe = &ProbeState{} }
					s.Probe.OK, s.Probe.Message = true, fmt.Sprintf("responde ao ping · %.1f ms", lat)
					s.LatencyMs = lat
				})
				h.SetOnline(d.ID, true, "")
			} else {
				fails++
				if fails >= 2 { h.SetOnline(d.ID, false, "não responde ao ping") } // 2 falhas seguidas = sem rede
			}
			select { case <-stop: return; case <-time.After(3 * time.Second): }
		}
	}()
	return func() { close(stop) }
}

// arpBlackmagic: pinga a faixa e devolve IP → MAC dos aparelhos Blackmagic (OUI 7C:2E:0D).
func arpBlackmagic(prefix string) map[string]string {
	var wg sync.WaitGroup
	sem := make(chan struct{}, 64)
	for i := 1; i < 255; i++ {
		ip := prefix + "." + strconv.Itoa(i)
		wg.Add(1)
		go func() { defer wg.Done(); sem <- struct{}{}; pingOnce(ip, 500*time.Millisecond); <-sem }()
	}
	wg.Wait()
	var cmd *exec.Cmd
	if runtime.GOOS == "linux" { cmd = exec.Command("ip", "neigh") } else { cmd = exec.Command("arp", "-an") }
	hideWindow(cmd)
	out, _ := cmd.CombinedOutput()
	res := map[string]string{}
	// macOS: "? (192.168.10.59) at 7c:2e:d:1:a:bb on en0" (octetos sem zero à esquerda)
	for _, m := range macArpRe.FindAllStringSubmatch(string(out), -1) {
		parts := strings.Split(m[2], ":")
		if len(parts) != 6 { continue }
		for i, p := range parts { if len(p) == 1 { parts[i] = "0" + p } }
		mac := strings.ToUpper(strings.Join(parts, ":"))
		if strings.HasPrefix(m[1], prefix+".") && strings.HasPrefix(mac, "7C:2E:0D") { res[m[1]] = mac }
	}
	for _, m := range arpRe.FindAllStringSubmatch(string(out), -1) {
		mac := strings.ToUpper(strings.ReplaceAll(m[2], "-", ":"))
		if strings.HasPrefix(m[1], prefix+".") && strings.HasPrefix(mac, "7C:2E:0D") { res[m[1]] = mac }
	}
	return res
}
