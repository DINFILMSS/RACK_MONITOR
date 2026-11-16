package main

// CONTROLE DE CÂMERA (CCU) — leitura dos ajustes que a ATEM repassa a todos os
// clientes (comando CCdP). Captura o que o ATEM Camera Control Panel, o
// ATEM Software Control ou o Companion mudam: íris, foco, ganho, balanço de
// branco, shutter, lift/gama/ganho de cor, contraste, saturação.
//
// EXPERIMENTAL: domínios e parâmetros seguem o protocolo SDI Camera Control da
// Blackmagic e o dissector Wireshark público; a posição dos valores (bytes 16+)
// segue implementações abertas e precisa ser validada numa mesa real.

import (
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

type CamState struct {
	Input   int               `json:"input"`
	Values  map[string]string `json:"values"`
	Updated time.Time         `json:"updated"`
	Changes int               `json:"changes"`
}

var ccuNames = map[[2]int]string{
	{0, 0}: "Foco", {0, 3}: "Íris", {0, 9}: "Zoom",
	{1, 1}: "Ganho", {1, 2}: "Balanço de branco", {1, 5}: "Shutter", {1, 13}: "Ganho dB", {1, 16}: "Filtro ND",
	{8, 0}: "Lift (preto)", {8, 1}: "Gama", {8, 2}: "Ganho de cor", {8, 3}: "Offset", {8, 4}: "Contraste", {8, 5}: "Luma mix", {8, 6}: "Matiz/Saturação",
}

func fx(b []byte, off int) float64 {
	if len(b) < off+2 { return math.NaN() }
	return float64(int16(binary.BigEndian.Uint16(b[off:]))) / 2048
}
func i16(b []byte, off int) int { if len(b) < off+2 { return 0 }; return int(int16(binary.BigEndian.Uint16(b[off:]))) }

// ccuDecode devolve (nome do parâmetro, valor legível, registrar?).
func ccuDecode(d []byte) (int, string, string, bool) {
	if len(d) < 18 { return 0, "", "", false }
	in, dom, feat := int(d[0]), int(d[1]), int(d[2])
	name, ok := ccuNames[[2]int{dom, feat}]
	if !ok { return in, fmt.Sprintf("Param %d.%d", dom, feat), fmt.Sprintf("%d", i16(d, 16)), false }
	v := d[16:]
	pct := func(f float64) string { return fmt.Sprintf("%.0f%%", f*100) }
	sg := func(f float64) string { return fmt.Sprintf("%+.2f", f) }
	switch [2]int{dom, feat} {
	case [2]int{0, 0}: return in, name, pct(fx(d, 16)), true
	case [2]int{0, 3}: return in, name, pct(fx(d, 16)), true
	case [2]int{0, 9}: return in, name, fmt.Sprintf("%.2f", fx(d, 16)), false // velocidade: não registra (geraria spam)
	case [2]int{1, 1}:
		g := i16(d, 16)
		if s, ok := map[int]string{512: "0 dB", 1024: "+6 dB", 2048: "+12 dB", 4096: "+18 dB"}[g]; ok { return in, name, s, true }
		return in, name, fmt.Sprintf("%d", g), true
	case [2]int{1, 2}: return in, name, fmt.Sprintf("%d K", i16(d, 16)), true
	case [2]int{1, 5}:
		us := 0
		if len(v) >= 4 { us = int(int32(binary.BigEndian.Uint32(v))) }
		if us > 0 { return in, name, fmt.Sprintf("1/%d", int(math.Round(1e6/float64(us)))), true }
		return in, name, "?", true
	case [2]int{1, 13}: return in, name, fmt.Sprintf("%+d dB", int(int8(v[0]))), true
	case [2]int{1, 16}: return in, name, fmt.Sprintf("%.1f", fx(d, 16)), true
	case [2]int{8, 0}, [2]int{8, 1}, [2]int{8, 2}, [2]int{8, 3}:
		return in, name, fmt.Sprintf("Y %s · R %s G %s B %s", sg(fx(d, 22)), sg(fx(d, 16)), sg(fx(d, 18)), sg(fx(d, 20))), true
	case [2]int{8, 4}: return in, name, pct(fx(d, 18)), true
	case [2]int{8, 5}: return in, name, pct(fx(d, 16)), true
	case [2]int{8, 6}: return in, name, fmt.Sprintf("matiz %s · sat %s", sg(fx(d, 16)), pct(fx(d, 18))), true
	}
	return in, name, "", false
}

// ccuNums: valores numéricos do ajuste e o limiar (mesma unidade) para registrar.
// pct = variação mínima configurada (0.05 = 5%).
func ccuNums(d []byte, pct float64) ([]float64, float64) {
	if len(d) < 18 { return nil, 0 }
	dom, feat := int(d[1]), int(d[2])
	switch [2]int{dom, feat} {
	case [2]int{0, 0}, [2]int{0, 3}, [2]int{8, 5}, [2]int{1, 16}:
		return []float64{fx(d, 16)}, pct
	case [2]int{8, 0}, [2]int{8, 1}, [2]int{8, 3}:
		return []float64{fx(d, 16), fx(d, 18), fx(d, 20), fx(d, 22)}, pct
	case [2]int{8, 2}: // ganho de cor vai até ~16: limiar proporcional (5% de 1.0)
		return []float64{fx(d, 16), fx(d, 18), fx(d, 20), fx(d, 22)}, pct
	case [2]int{8, 4}:
		return []float64{fx(d, 18)}, pct
	case [2]int{8, 6}:
		return []float64{fx(d, 16), fx(d, 18)}, pct
	case [2]int{1, 2}:
		return []float64{float64(i16(d, 16))}, 150 // balanço de branco: 150 K
	}
	return []float64{float64(i16(d, 16)), float64(i16(d, 18))}, 0.5 // ganho, shutter, dB: qualquer passo
}

func maxDiff(a, b []float64) float64 {
	m := 0.0
	for i := range a { if i < len(b) { if x := math.Abs(a[i] - b[i]); x > m { m = x } } }
	return m
}

type ccuBase struct {
	nums []float64
	str  string
}

// --- registro com "debounce": um botão girando gera dezenas de pacotes;
// a linha do tempo recebe só o valor final de cada ajuste (de → para).
type ccuPend struct {
	from, to string
	last     time.Time
}

type ccuLogger struct {
	mu   sync.Mutex
	pend map[string]*ccuPend // "input|param"
}

func (l *ccuLogger) note(in int, param, from, to string) {
	l.mu.Lock(); defer l.mu.Unlock()
	if l.pend == nil { l.pend = map[string]*ccuPend{} }
	k := fmt.Sprintf("%d|%s", in, param)
	p, ok := l.pend[k]
	if !ok { p = &ccuPend{from: from}; l.pend[k] = p }
	p.to, p.last = to, time.Now()
}

func (c *atemClient) ccuFlushLoop() {
	t := time.NewTicker(700 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-c.stop: return
		case <-t.C:
		}
		c.ccu.mu.Lock()
		var out []string
		type item struct{ in int; param, from, to string }
		var items []item
		for k, p := range c.ccu.pend {
			if time.Since(p.last) < 1500*time.Millisecond { continue }
			parts := strings.SplitN(k, "|", 2)
			var in int
			fmt.Sscanf(parts[0], "%d", &in)
			if p.from != p.to { items = append(items, item{in, parts[1], p.from, p.to}) }
			out = append(out, k)
		}
		for _, k := range out { delete(c.ccu.pend, k) }
		c.ccu.mu.Unlock()
		sort.Slice(items, func(a, b int) bool { return items[a].in < items[b].in })
		for _, it := range items {
			name := ""
			c.h.Update(c.dev.ID, func(s *DevState) {
				if s.Atem != nil { name = s.Atem.Name(it.in); if cs := s.Atem.Cams[it.in]; cs != nil { cs.Changes++ } }
			})
			from := it.from
			if from == "" { from = "?" }
			c.h.LogO("externo", c.dev.ID, c.dev.Name, "CONTROLE DE CÂMERA", "info",
				fmt.Sprintf("%s · %s: %s → %s", name, it.param, from, it.to), map[string]any{"input": it.in, "param": it.param, "to": it.to})
		}
	}
}
