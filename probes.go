package main

// Sondas genéricas (qualquer equipamento IP), gerência dos drivers,
// integração com Companion (variáveis personalizadas) e webhook.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

func startProbe(h *Hub, d Device) func() {
	stop := make(chan struct{})
	go func() {
		t := time.NewTicker(3 * time.Second)
		defer t.Stop()
		for {
			ok, lat, code, msg := probeOnce(d)
			h.Update(d.ID, func(s *DevState) {
				if s.Probe == nil { s.Probe = &ProbeState{} }
				s.Probe.OK, s.Probe.Code, s.Probe.Message = ok || code > 0, code, msg
				if ok || code > 0 { s.LatencyMs = lat }
			})
			if d.Kind == "http" {
				h.SetOnline(d.ID, code > 0, msg)
				h.Update(d.ID, func(s *DevState) { s.Probe.OK = code >= 200 && code < 400 })
			} else {
				h.SetOnline(d.ID, ok, msg)
			}
			select { case <-stop: return; case <-t.C: }
		}
	}()
	return func() { close(stop) }
}

func probeOnce(d Device) (bool, float64, int, string) {
	t0 := time.Now()
	if d.Kind == "http" {
		u := d.Host
		if !strings.HasPrefix(u, "http") { u = "http://" + net.JoinHostPort(d.Host, strconv.Itoa(d.Port)) + "/" }
		cl := http.Client{Timeout: 2500 * time.Millisecond}
		resp, err := cl.Get(u)
		if err != nil { return false, 0, 0, errText(err) }
		resp.Body.Close()
		lat := float64(time.Since(t0).Microseconds()) / 1000
		return resp.StatusCode < 400, lat, resp.StatusCode, fmt.Sprintf("HTTP %d em %.0f ms", resp.StatusCode, lat)
	}
	c, err := net.DialTimeout("tcp", net.JoinHostPort(d.Host, strconv.Itoa(d.Port)), 2*time.Second)
	if err != nil { return false, 0, 0, errText(err) }
	c.Close()
	lat := float64(time.Since(t0).Microseconds()) / 1000
	return true, lat, 0, fmt.Sprintf("porta %d aberta · %.1f ms", d.Port, lat)
}

// ---------- gerência de drivers ----------

var driverMu sync.Mutex

func (h *Hub) applyDevices() {
	driverMu.Lock(); defer driverMu.Unlock()
	cfgMu.RLock()
	devs := append([]Device(nil), h.cfg.Devices...)
	cfgMu.RUnlock()
	want := map[string]Device{}
	h.mu.Lock()
	for _, d := range devs { want[d.ID] = d; h.ensureState(d) }
	for id := range h.states { if _, ok := want[id]; !ok { delete(h.states, id) } }
	h.dirty = true
	h.mu.Unlock()
	// para drivers removidos/alterados
	for id, stop := range h.drivers {
		d, ok := want[id]
		sig := ""
		if ok { sig = driverSig(d) }
		if !ok || h.driverSig[id] != sig { stop(); delete(h.drivers, id); delete(h.driverSig, id) }
	}
	for _, d := range devs {
		if !d.Enabled || d.Host == "" { continue }
		if _, ok := h.drivers[d.ID]; ok { continue }
		h.mu.Lock()
		if s := h.states[d.ID]; s != nil { s.Status = "CONECTANDO"; s.Online = false; s.Since = time.Now(); s.Atem, s.Deck, s.Hub, s.Probe = nil, nil, nil, nil }
		h.mu.Unlock()
		var stop func()
		switch d.Kind {
		case "atem": stop = startAtem(h, d)
		case "hyperdeck":
			if d.Via == "companion" { stop = startDeckViaCompanion(h, d) } else { stop = startHyperdeck(h, d) }
		case "videohub": stop = startVideohub(h, d)
		case "tcp", "http": stop = startProbe(h, d)
		case "ping": stop = startPing(h, d)
		case "bmcamera": stop = startBMCamera(h, d)
		case "webpresenter": stop = startWebPresenter(h, d)
		case "ups", "switch": stop = startSNMP(h, d)
		default: continue
		}
		h.drivers[d.ID] = stop
		h.driverSig[d.ID] = driverSig(d)
	}
}

func driverSig(d Device) string { return fmt.Sprintf("%s|%s|%d|%v|%s|%s|%s|%s", d.Kind, d.Host, d.Port, d.Enabled, d.Community, d.Ports, d.Via, d.CompLabel) }

// ---------- varredura de rede ----------

type Found struct {
	Host string `json:"host"`
	Kind string `json:"kind"`
	Port int    `json:"port"`
	Info string `json:"info"`
}

func scanNetwork(prefix string) []Found {
	prefix = strings.TrimSpace(prefix)
	prefix = strings.TrimSuffix(prefix, ".0/24")
	prefix = strings.TrimSuffix(prefix, ".")
	parts := strings.Split(prefix, ".")
	if len(parts) != 3 { return nil }
	var mu sync.Mutex
	var out []Found
	var wg sync.WaitGroup
	sem := make(chan struct{}, 96)
	for i := 1; i < 255; i++ {
		ip := prefix + "." + strconv.Itoa(i)
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}; defer func() { <-sem }()
			for _, k := range []struct{ kind string; port int }{{"hyperdeck", 9993}, {"videohub", 9990}} {
				if c, err := net.DialTimeout("tcp", net.JoinHostPort(ip, strconv.Itoa(k.port)), 600*time.Millisecond); err == nil {
					c.SetReadDeadline(time.Now().Add(900 * time.Millisecond))
					b := make([]byte, 512)
					n, _ := c.Read(b)
					c.Close()
					info := strings.ReplaceAll(strings.TrimSpace(string(b[:n])), "\r", "")
					if i := strings.Index(info, "\n\n"); i > 0 { info = info[:i] }
					mu.Lock(); out = append(out, Found{ip, k.kind, k.port, firstLines(info, 3)}); mu.Unlock()
				}
			}
			if atemDiscover(ip, 9910, 700*time.Millisecond) {
				mu.Lock(); out = append(out, Found{ip, "atem", 9910, "responde ao protocolo ATEM (UDP 9910)"}); mu.Unlock()
			}
			if c, err := net.DialTimeout("tcp", net.JoinHostPort(ip, "8000"), 400*time.Millisecond); err == nil {
				c.Close()
				mu.Lock(); out = append(out, Found{ip, "http", 8000, "porta 8000 aberta (Companion?)"}); mu.Unlock()
			}
		}()
	}
	wg.Wait()
	// tudo que é Blackmagic pelo MAC e ainda não apareceu (painéis, câmeras, conversores…)
	seen := map[string]bool{}
	for _, f := range out { seen[f.Host] = true }
	for ip, mac := range arpBlackmagic(prefix) {
		if !seen[ip] { out = append(out, Found{ip, "ping", 0, "Blackmagic (MAC " + mac + ") sem porta de controle conhecida — provável painel ATEM, Camera Control Panel, câmera ou conversor"}) } else {
			for i := range out { if out[i].Host == ip { out[i].Info += " · MAC " + mac } }
		}
	}
	return out
}

func firstLines(s string, n int) string {
	ls := strings.Split(s, "\n")
	if len(ls) > n { ls = ls[:n] }
	return strings.Join(ls, " · ")
}

// ---------- Companion ----------

func (h *Hub) companionVars() map[string]string {
	h.mu.Lock(); defer h.mu.Unlock()
	v := map[string]string{}
	crit, warn := 0, 0
	top := ""
	for _, a := range h.alerts {
		if a.Acked { continue }
		if a.Sev == "crit" { crit++; if top == "" { top = a.DevName + ": " + a.Title } } else if a.Sev == "warn" { warn++ }
	}
	v["alerts_crit"] = strconv.Itoa(crit)
	v["alerts_warn"] = strconv.Itoa(warn)
	v["alert_top"] = top
	st := "OK"
	if crit > 0 { st = "CRITICO" } else if warn > 0 { st = "ATENCAO" }
	v["status"] = st
	v["rec_watch"] = map[bool]string{true: "ARMADA", false: "DESARMADA"}[h.recArmed]
	if h.job != nil { v["job"] = h.job.Name }
	for _, d := range h.cfg.Devices {
		s := h.states[d.ID]
		if s == nil { continue }
		p := d.ID + "_"
		v[p+"online"] = map[bool]string{true: "1", false: "0"}[s.Online]
		v[p+"status"] = s.Status
		if dk := s.Deck; dk != nil {
			v[p+"rec"] = map[bool]string{true: "1", false: "0"}[dk.Recording()]
			v[p+"transport"] = strings.ToUpper(dk.Transport)
			v[p+"tc"] = dk.TC
			v[p+"remain"] = hms(dk.TotalRemain)
			v[p+"remain_min"] = strconv.Itoa(dk.TotalRemain / 60)
		}
		if at := s.Atem; at != nil {
			for i, m := range at.MEs {
				v[fmt.Sprintf("%sme%d_pgm", p, i+1)] = at.Name(m.Program)
				v[fmt.Sprintf("%sme%d_pvw", p, i+1)] = at.Name(m.Preview)
				v[fmt.Sprintf("%sme%d_ftb", p, i+1)] = map[bool]string{true: "1", false: "0"}[m.FTB]
			}
			am := h.cfg.auxMap(d.ID)
			for i, src := range at.Aux { if am[i].Mode != "ignore" { v[fmt.Sprintf("%saux%d", p, i+1)] = at.Name(src) } }
			for in, cs := range at.Cams {
				for k, val := range map[string]string{"iris": cs.Values["Íris"], "gain": cs.Values["Ganho"], "wb": cs.Values["Balanço de branco"], "shutter": cs.Values["Shutter"]} {
					if val != "" { v[fmt.Sprintf("%scam%d_%s", p, in, k)] = val }
				}
			}
			if at.Stream != "" { v[p+"stream"] = at.Stream }
			if at.Rec != "" { v[p+"rec"] = at.Rec }
		}
		if hb := s.Hub; hb != nil {
			v[p+"changes"] = strconv.Itoa(hb.Changes)
		}
	}
	pv := map[string]string{}
	for k, x := range v { pv[h.cfg.Companion.Prefix+k] = x }
	return pv
}

func (h *Hub) companionLoop() {
	sent := map[string]string{}
	cl := http.Client{Timeout: 1500 * time.Millisecond}
	failUntil := time.Time{}
	for range time.Tick(time.Second) {
		cfgMu.RLock()
		cc := h.cfg.Companion
		cfgMu.RUnlock()
		if !cc.Enabled || cc.Host == "" { sent = map[string]string{}; continue }
		if time.Now().Before(failUntil) { continue }
		for k, val := range h.companionVars() {
			if sent[k] == val { continue }
			u := fmt.Sprintf("http://%s/api/custom-variable/%s/value?value=%s", net.JoinHostPort(cc.Host, strconv.Itoa(cc.Port)), url.PathEscape(k), url.QueryEscape(val))
			resp, err := cl.Post(u, "text/plain", nil)
			if err != nil { failUntil = time.Now().Add(10 * time.Second); log.Printf("Companion indisponível: %v", err); break }
			resp.Body.Close()
			sent[k] = val
		}
	}
}

func (h *Hub) webhook(key, sev, title, dev, msg string) {
	go h.telegramAlert(sev, title, dev, msg)
	cfgMu.RLock()
	u := h.cfg.WebhookURL
	st := h.cfg.StationName
	cfgMu.RUnlock()
	if u == "" || sev == "info" { return }
	b, _ := json.Marshal(map[string]any{"app": "RACK MONITOR BY DON", "station": st, "key": key, "severity": sev, "title": title, "device": dev, "message": msg, "at": time.Now(),
		"content": fmt.Sprintf("[%s] %s — %s: %s", strings.ToUpper(sev), dev, title, msg), "text": fmt.Sprintf("[%s] %s — %s: %s", strings.ToUpper(sev), dev, title, msg)})
	cl := http.Client{Timeout: 3 * time.Second}
	if resp, err := cl.Post(u, "application/json", bytes.NewReader(b)); err == nil { resp.Body.Close() }
}
