package main

// DIAGNÓSTICO E SAÚDE
// • Gravador de diagnóstico: grava por N segundos tudo o que os equipamentos
//   mandam (comandos ATEM em hexadecimal, respostas HyperDeck/VideoHub) num
//   arquivo de texto — serve para validar/ajustar o software no rack real.
// • Histórico de latência/disponibilidade por equipamento (últimas 2 h).

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

var diagOn atomic.Bool

type diagRec struct {
	mu    sync.Mutex
	f     *os.File
	path  string
	until time.Time
	lines int
	start time.Time
}

var diag diagRec

func (h *Hub) StartDiag(sec int) (string, error) {
	if sec <= 0 || sec > 600 { sec = 60 }
	diag.mu.Lock(); defer diag.mu.Unlock()
	if diag.f != nil { return diag.path, fmt.Errorf("já existe uma captura em andamento") }
	dir := filepath.Join(h.paths.Root, "diagnostico")
	_ = os.MkdirAll(dir, 0o755)
	p := filepath.Join(dir, "DIAG_"+time.Now().Format("20060102_150405")+".txt")
	f, err := os.Create(p)
	if err != nil { return "", err }
	diag.f, diag.path, diag.until, diag.lines, diag.start = f, p, time.Now().Add(time.Duration(sec)*time.Second), 0, time.Now()
	cfgMu.RLock()
	fmt.Fprintf(f, "RACK MONITOR BY DON V%s — captura de diagnóstico\r\nInício: %s · duração: %d s · estação: %s\r\n", AppVersion, time.Now().Format("02/01/2006 15:04:05"), sec, h.cfg.StationName)
	for _, d := range h.cfg.Devices { fmt.Fprintf(f, "EQUIP %s | %s | %s | %s:%d\r\n", d.ID, d.Name, d.Kind, d.Host, d.Port) }
	cfgMu.RUnlock()
	h.mu.Lock()
	for id, s := range h.states {
		if s.Atem != nil { fmt.Fprintf(f, "ATEM %s | produto %q | protocolo %s | %d M/E | %d AUX | %d fontes\r\n", id, s.Atem.Product, s.Atem.Version, len(s.Atem.MEs), len(s.Atem.Aux), len(s.Atem.Inputs)) }
		if s.Deck != nil { fmt.Fprintf(f, "DECK %s | %s | protocolo %s | sw %s\r\n", id, s.Deck.Model, s.Deck.Protocol, s.Deck.Software) }
		if s.Hub != nil { fmt.Fprintf(f, "HUB %s | %s | %dx%d\r\n", id, s.Hub.Model, s.Hub.Inputs, s.Hub.Outputs) }
	}
	h.mu.Unlock()
	f.WriteString("----\r\n")
	diagOn.Store(true)
	go func() {
		time.Sleep(time.Duration(sec) * time.Second)
		h.StopDiag()
	}()
	return p, nil
}

func (h *Hub) StopDiag() {
	diag.mu.Lock()
	if diag.f == nil { diag.mu.Unlock(); return }
	diagOn.Store(false)
	fmt.Fprintf(diag.f, "----\r\nFim: %s · %d linhas\r\n", time.Now().Format("15:04:05"), diag.lines)
	diag.f.Close()
	p, n := diag.path, diag.lines
	diag.f = nil
	diag.mu.Unlock()
	h.Log("", "RACK MONITOR", "DIAGNÓSTICO GRAVADO", "info", fmt.Sprintf("%d linhas em %s", n, filepath.Base(p)), map[string]any{"file": filepath.Base(p)})
}

func diagWrite(dev, kind, text string) {
	if !diagOn.Load() { return }
	diag.mu.Lock(); defer diag.mu.Unlock()
	if diag.f == nil { return }
	if diag.lines > 400000 { return }
	diag.lines++
	fmt.Fprintf(diag.f, "%s %-28s %-6s %s\r\n", time.Now().Format("15:04:05.000"), dev, kind, text)
}

func diagHex(b []byte) string {
	if len(b) > 512 { b = b[:512] }
	return hex.EncodeToString(b)
}

func (h *Hub) DiagFiles() []map[string]any {
	dir := filepath.Join(h.paths.Root, "diagnostico")
	ents, _ := os.ReadDir(dir)
	out := []map[string]any{}
	for _, e := range ents {
		if e.IsDir() { continue }
		fi, _ := e.Info()
		out = append(out, map[string]any{"name": e.Name(), "size": fi.Size(), "at": fi.ModTime()})
	}
	sort.Slice(out, func(a, b int) bool { return out[a]["name"].(string) > out[b]["name"].(string) })
	diag.mu.Lock()
	running := diag.f != nil
	left := 0
	if running { left = int(time.Until(diag.until).Seconds()) }
	diag.mu.Unlock()
	return append([]map[string]any{{"running": running, "left": left}}, out...)
}

// ---------- histórico de saúde ----------

type HealthSample struct {
	T   int64   `json:"t"`
	Up  bool    `json:"up"`
	Lat float64 `json:"lat"`
}

type healthHist struct {
	mu   sync.Mutex
	data map[string][]HealthSample
}

var hist = healthHist{data: map[string][]HealthSample{}}

func (h *Hub) sampleHealth() {
	h.mu.Lock()
	now := time.Now().Unix()
	snap := map[string]HealthSample{}
	for id, s := range h.states { if s.Enabled { snap[id] = HealthSample{now, s.Online, s.LatencyMs} } }
	h.mu.Unlock()
	hist.mu.Lock()
	for id, x := range snap {
		l := append(hist.data[id], x)
		if len(l) > 720 { l = l[len(l)-720:] }
		hist.data[id] = l
	}
	hist.mu.Unlock()
}

// comandos ATEM vistos (para descobrir o que a mesa manda e o monitor ainda não interpreta)
var atemSeen = struct {
	mu sync.Mutex
	m  map[string]map[string]int
}{m: map[string]map[string]int{}}

var atemKnown = map[string]bool{"_ver": true, "_pin": true, "InPr": true, "PrgI": true, "PrvI": true, "AuxS": true, "TrPs": true, "FtbS": true, "KeOn": true, "DskS": true, "TlSr": true, "StRS": true, "SRST": true, "RTMS": true, "RTMR": true, "MPrp": true, "MRPr": true, "Warn": true, "InCm": true, "CCdP": true}

func atemCount(dev, name string) {
	atemSeen.mu.Lock()
	m := atemSeen.m[dev]
	if m == nil { m = map[string]int{}; atemSeen.m[dev] = m }
	m[name]++
	atemSeen.mu.Unlock()
}

func (h *Hub) Health() map[string]any {
	type dv struct {
		ID         string         `json:"id"`
		Name       string         `json:"name"`
		Kind       string         `json:"kind"`
		Online     bool           `json:"online"`
		Reconnects int            `json:"reconnects"`
		Lat        float64        `json:"lat"`
		Samples    []HealthSample `json:"samples"`
		Extra      string         `json:"extra"`
		Cmds       map[string]int `json:"cmds,omitempty"`
		Unknown    []string       `json:"unknown,omitempty"`
	}
	out := []dv{}
	h.mu.Lock()
	for _, d := range h.cfg.Devices {
		s := h.states[d.ID]
		if s == nil || !d.Enabled { continue }
		x := dv{ID: d.ID, Name: d.Name, Kind: d.Kind, Online: s.Online, Reconnects: s.Reconnects, Lat: s.LatencyMs}
		if s.Atem != nil { x.Extra = fmt.Sprintf("%s · protocolo %s · %d pacotes recebidos", s.Atem.Product, s.Atem.Version, s.Atem.PacketsIn) }
		if s.Deck != nil { x.Extra = fmt.Sprintf("%s · protocolo %s · software %s", s.Deck.Model, s.Deck.Protocol, s.Deck.Software) }
		if s.Hub != nil { x.Extra = fmt.Sprintf("%s · %d×%d · %d trocas", s.Hub.Model, s.Hub.Inputs, s.Hub.Outputs, s.Hub.Changes) }
		out = append(out, x)
	}
	h.mu.Unlock()
	hist.mu.Lock()
	for i := range out { out[i].Samples = append([]HealthSample(nil), hist.data[out[i].ID]...) }
	hist.mu.Unlock()
	atemSeen.mu.Lock()
	for i := range out {
		if m := atemSeen.m[out[i].ID]; m != nil {
			out[i].Cmds = map[string]int{}
			for k, v := range m { out[i].Cmds[k] = v; if !atemKnown[k] { out[i].Unknown = append(out[i].Unknown, k) } }
			sort.Strings(out[i].Unknown)
		}
	}
	atemSeen.mu.Unlock()
	return map[string]any{"devices": out, "diag": h.DiagFiles()}
}
