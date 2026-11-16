package main

// HYPERDECK "VIA COMPANION"
// Vários HyperDecks (ex.: Studio Mini, Studio 12G) aceitam UMA conexão de
// controle. Se o Companion já está conectado, o monitor não disputa a vaga:
// lê o estado pelas variáveis do módulo HyperDeck do Companion
// (GET /api/variable/<conexão>/<variável>/value). Somente leitura.

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var compHTTP = &http.Client{Timeout: 1500 * time.Millisecond, Transport: &http.Transport{MaxIdleConnsPerHost: 32, IdleConnTimeout: 30 * time.Second}}

// nomes possíveis de cada informação (mudam entre versões do módulo)
var compVarNames = map[string][]string{
	"status":  {"status", "transport", "transportStatus"},
	"tc":      {"timecodeHMSF", "displayTimecode", "timecode"},
	"slot":    {"slotId", "slotid", "slot_id"},
	"clip":    {"clipId", "clipid", "clipName"},
	"format":  {"videoFormat", "format"},
	"remain":  {"recordingTime", "remainingTime", "recordTimeRemaining", "slotRecordingTime", "recording_time"},
	"remain1": {"slot1_recordingTime", "slot1RecordingTime", "recordingTime1", "slot1_remaining"},
	"remain2": {"slot2_recordingTime", "slot2RecordingTime", "recordingTime2", "slot2_remaining"},
	"codec":   {"fileFormat", "codec"},
}

func compBase() (string, bool) {
	cfgMu.RLock(); defer cfgMu.RUnlock()
	c := globalHub.cfg.Companion
	host := nz(c.Host, "127.0.0.1")
	port := c.Port
	if port == 0 { port = 8000 }
	return "http://" + net.JoinHostPort(host, strconv.Itoa(port)), true
}

var globalHub *Hub

// compGet: valor de uma variável do módulo; "", false se não existir.
func compGet(base, label, name string) (string, bool, error) {
	u := base + "/api/variable/" + url.PathEscape(label) + "/" + url.PathEscape(name) + "/value"
	r, err := compHTTP.Get(u)
	if err != nil { return "", false, err }
	b, _ := io.ReadAll(io.LimitReader(r.Body, 4096))
	r.Body.Close()
	if r.StatusCode != 200 { return "", false, nil }
	v := strings.TrimSpace(string(b))
	if strings.HasPrefix(v, "<") { return "", false, nil } // Companion antigo devolve a página HTML
	return v, v != "", nil
}

var hmsRe = regexp.MustCompile(`^(\d+):(\d{1,2})(?::(\d{1,2}))?`)

// parseRemain: "4:17:07", "257:07", "15430" (s) ou "257 min" → segundos
func parseRemain(v string) (int, bool) {
	v = strings.TrimSpace(strings.ToLower(v))
	if v == "" { return 0, false }
	if m := hmsRe.FindStringSubmatch(v); m != nil {
		a, _ := strconv.Atoi(m[1]); b, _ := strconv.Atoi(m[2])
		if m[3] != "" { c, _ := strconv.Atoi(m[3]); return a*3600 + b*60 + c, true }
		return a*60 + b, true
	}
	n, err := strconv.Atoi(strings.Fields(v)[0])
	if err != nil { return 0, false }
	if strings.Contains(v, "min") { return n * 60, true }
	return n, true
}

func normStatus(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	switch {
	case strings.Contains(v, "rec"): return "record"
	case strings.Contains(v, "stop"): return "stopped"
	case strings.Contains(v, "play"): return "play"
	case strings.Contains(v, "prev"): return "preview"
	case strings.Contains(v, "shuttle"): return "shuttle"
	case strings.Contains(v, "jog"): return "jog"
	case strings.Contains(v, "forw"): return "forward"
	case strings.Contains(v, "rew"): return "rewind"
	}
	return v
}

func startDeckViaCompanion(h *Hub, d Device) func() {
	stop := make(chan struct{})
	go func() {
		found := map[string]string{} // chave → nome que funcionou
		n := 0
		for {
			base, _ := compBase()
			label := strings.TrimSpace(d.CompLabel)
			if label == "" {
				h.SetOnline(d.ID, false, "Falta o nome da conexão do Companion: CONFIGURAÇÃO → linha deste HyperDeck → \"Nome da conexão no Companion\" (ou TESTAR / ESCOLHER).")
				select { case <-stop: return; case <-time.After(3 * time.Second): }
				continue
			}
			t0 := time.Now()
			vals := map[string]string{}
			var mu sync.Mutex
			var wg sync.WaitGroup
			var netErr error
			for key, names := range compVarNames {
				// remaining/codec a cada 5 s; o resto a cada segundo
				if (strings.HasPrefix(key, "remain") || key == "codec") && n%5 != 0 { continue }
				wg.Add(1)
				go func(key string, names []string) {
					defer wg.Done()
					try := names
					if nm, ok := found[key]; ok { try = []string{nm} }
					for _, nm := range try {
						v, ok, err := compGet(base, label, nm)
						if err != nil { mu.Lock(); netErr = err; mu.Unlock(); return }
						if ok { mu.Lock(); vals[key] = v; found[key] = nm; mu.Unlock(); return }
					}
				}(key, names)
			}
			wg.Wait()
			n++
			lat := float64(time.Since(t0).Microseconds()) / 1000
			switch {
			case netErr != nil:
				h.SetOnline(d.ID, false, "Companion não responde em "+base+" ("+errText(netErr)+"). Confira IP/porta em CONFIGURAÇÃO → COMPANION.")
			case vals["status"] == "":
				h.SetOnline(d.ID, false, fmt.Sprintf("Companion sem dados da conexão \"%s\" (nome errado, conexão desativada ou sem contato com o HyperDeck).", label))
			default:
				h.Update(d.ID, func(s *DevState) {
					if s.Deck == nil { s.Deck = &DeckState{Slots: map[int]*DeckSlot{}, Model: "HyperDeck (via Companion)"} }
					s.LatencyMs = lat
				})
				dc := &deckClient{h: h, dev: d}
				h.SetOnline(d.ID, true, "")
				p := map[string]string{"status": normStatus(vals["status"])}
				slot := 1
				if v := vals["slot"]; v != "" { if x, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(strings.ToLower(v), "slot"))); err == nil && x > 0 { slot = x } }
				p["slot id"] = strconv.Itoa(slot)
				if v := vals["tc"]; v != "" { p["display timecode"] = v }
				if v := vals["clip"]; v != "" { p["clip id"] = v }
				if v := vals["format"]; v != "" { p["video format"] = strings.ReplaceAll(v, " ", "") }
				dc.apply(block{code: 508, props: p})
				if v := vals["codec"]; v != "" { dc.apply(block{code: 511, props: map[string]string{"file format": v}}) }
				for i, key := range []string{"remain1", "remain2"} {
					if sec, ok := parseRemain(vals[key]); ok {
						dc.apply(block{code: 502, props: map[string]string{"slot id": strconv.Itoa(i + 1), "status": "mounted", "recording time": strconv.Itoa(sec)}})
					}
				}
				if _, ok := parseRemain(vals["remain1"]); !ok {
					if sec, ok := parseRemain(vals["remain"]); ok {
						dc.apply(block{code: 502, props: map[string]string{"slot id": strconv.Itoa(slot), "status": "mounted", "recording time": strconv.Itoa(sec)}})
					}
				}
				h.Update(d.ID, func(s *DevState) {
					miss := []string{}
					if found["tc"] == "" { miss = append(miss, "timecode (ative \"Timecode Variables\" = Polling na conexão do Companion)") }
					if found["remain"] == "" && found["remain1"] == "" { miss = append(miss, "tempo restante") }
					s.Detail = "lendo pelo Companion · conexão \"" + label + "\""
					if len(miss) > 0 { s.Detail += " · sem: " + strings.Join(miss, ", ") }
				})
			}
			select { case <-stop: return; case <-time.After(time.Second): }
		}
	}()
	return func() { close(stop) }
}

// compTest: diagnóstico para o botão TESTAR da configuração.
func compTest(label string) map[string]any {
	base, _ := compBase()
	out := map[string]any{"base": base, "label": label}
	res := []map[string]string{}
	reach := true
	for _, key := range []string{"status", "tc", "slot", "clip", "format", "codec", "remain", "remain1", "remain2"} {
		got, used := "", ""
		for _, nm := range compVarNames[key] {
			v, ok, err := compGet(base, label, nm)
			if err != nil { reach = false; break }
			if ok { got, used = v, nm; break }
		}
		res = append(res, map[string]string{"key": key, "variable": used, "value": got})
		if !reach { break }
	}
	out["reachable"] = reach
	out["vars"] = res
	// lista de conexões (Companion 4.3+)
	if r, err := compHTTP.Get(base + "/api/connections"); err == nil {
		b, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		r.Body.Close()
		if r.StatusCode == 200 && len(b) > 0 && b[0] == '[' { out["connections"] = string(b) }
	}
	return out
}
