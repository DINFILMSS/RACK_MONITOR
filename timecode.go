package main

// Timecode: interpolação do TC dos HyperDecks entre consultas (precisão de
// frame), marcadores do operador e EDL (CMX 3600) do corte do PGM.

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var vfRe = regexp.MustCompile(`(?i)(p|i|psf)(\d{2,4})`)

// fpsOf: "1080p5994" → 59.94 ; "1080i50" → 25 ; "2160p25" → 25
func fpsOf(vf string) float64 {
	m := vfRe.FindStringSubmatch(vf)
	if m == nil { return 0 }
	n, _ := strconv.Atoi(m[2])
	f := float64(n)
	if n >= 1000 { f = f / 100 }
	if strings.ToLower(m[1]) == "i" { f /= 2 }
	return f
}

func tcBase(fps float64) int { b := int(math.Round(fps)); if b <= 0 { b = 30 }; return b }

func tcToFrames(tc string, base int) (int, bool) {
	tc = strings.ReplaceAll(tc, ";", ":")
	p := strings.Split(tc, ":")
	if len(p) != 4 { return 0, false }
	v := make([]int, 4)
	for i, s := range p { n, err := strconv.Atoi(s); if err != nil { return 0, false }; v[i] = n }
	return ((v[0]*60+v[1])*60+v[2])*base + v[3], true
}

func framesToTC(f, base int) string {
	day := 24 * 3600 * base
	f = ((f % day) + day) % day
	return fmt.Sprintf("%02d:%02d:%02d:%02d", f/(3600*base), f/(60*base)%60, f/base%60, f%base)
}

// tcNowLocked: TC atual estimado (chamar com h.mu travado).
func tcNow(d *DeckState) string {
	if d == nil || d.TC == "" { return "" }
	if !d.Recording() || d.tcAt.IsZero() { return d.TC }
	fps := fpsOf(d.VideoFormat)
	if fps <= 0 { return d.TC }
	base := tcBase(fps)
	f, ok := tcToFrames(d.TC, base)
	if !ok { return d.TC }
	el := time.Since(d.tcAt).Seconds()
	if el < 0 || el > 5 { return d.TC }
	return framesToTC(f+int(el*fps), base)
}

// tcSnapshotLocked: TC de todos os gravadores conectados (chamar com h.mu travado).
func (h *Hub) tcSnapshotLocked() map[string]string {
	m := map[string]string{}
	for id, s := range h.states {
		if s.Online && s.Deck != nil && s.Deck.TC != "" {
			t := tcNow(s.Deck)
			if s.Deck.Recording() { t += "*" } // * = gravando
			m[id] = t
		}
	}
	return m
}

func (h *Hub) TCSnapshot() map[string]string { h.mu.Lock(); defer h.mu.Unlock(); return h.tcSnapshotLocked() }

// ---------- marcadores ----------

func (h *Hub) Marker(label, note, origin string) Event {
	label = strings.TrimSpace(label)
	if label == "" { label = "MARCADOR" }
	if len(label) > 60 { label = label[:60] }
	h.mu.Lock()
	tcs := h.tcSnapshotLocked()
	ref := ""
	for _, p := range h.cfg.Pairs { if t, ok := tcs[p.Primary]; ok { ref = h.states[p.Primary].Name + " " + strings.TrimSuffix(t, "*"); break } }
	if ref == "" {
		ids := make([]string, 0, len(tcs))
		for id := range tcs { ids = append(ids, id) }
		sort.Strings(ids)
		for _, id := range ids { if strings.HasSuffix(tcs[id], "*") { ref = h.states[id].Name + " " + strings.TrimSuffix(tcs[id], "*"); break } }
	}
	pgm := ""
	for _, d := range h.cfg.Devices {
		if s := h.states[d.ID]; s != nil && s.Atem != nil { if m := s.Atem.MEs[0]; m != nil { pgm = s.Atem.Name(m.Program); break } }
	}
	h.mu.Unlock()
	msg := strings.ToUpper(label)
	if note != "" { msg += " — " + note }
	if ref != "" { msg += " · TC " + ref }
	if pgm != "" { msg += " · no ar: " + pgm }
	h.LogO(origin, "", "MARCADOR", "MARCADOR", "warn", msg, map[string]any{"label": strings.ToUpper(label), "note": note, "tcs": tcs, "pgm": pgm})
	return Event{Type: "MARCADOR", Msg: msg}
}

// ---------- EDL ----------

func reelName(s string) string {
	s = strings.ToUpper(slugRe.ReplaceAllString(strings.ToLower(s), "_"))
	s = strings.Trim(s, "_")
	if len(s) > 8 { s = s[:8] }
	if s == "" { s = "AX" }
	return s
}

type edlSeg struct {
	at       time.Time
	name     string
	recIn    string
	hasTC    bool
}

// BuildEDL: corte do PGM (ATEM/M-E) sobre o TC do gravador de referência.
func (h *Hub) BuildEDL(jobID, atemID string, me int, deckID string) (string, int, error) {
	j, err := h.readJob(jobID)
	if err != nil { return "", 0, err }
	evs, err := h.ReadEvents(jobID)
	if err != nil { return "", 0, err }
	deckName, fps := deckID, 0.0
	h.mu.Lock()
	if s := h.states[deckID]; s != nil { deckName = s.Name; if s.Deck != nil { fps = fpsOf(s.Deck.VideoFormat) } }
	h.mu.Unlock()
	if fps == 0 { fps = 30 }
	base := tcBase(fps)
	var segs []edlSeg
	var markers []Event
	for _, e := range evs {
		tcs, _ := e.Data["tcs"].(map[string]any)
		if e.Type == "MARCADOR" { markers = append(markers, e); continue }
		if e.Type != "PGM" || e.Device != atemID || toInt(e.Data["me"]) != me { continue }
		name, _ := e.Data["name"].(string)
		t, _ := tcs[deckID].(string)
		if len(segs) > 0 && segs[len(segs)-1].name == name { continue }
		segs = append(segs, edlSeg{at: e.At, name: name, recIn: strings.TrimSuffix(t, "*"), hasTC: strings.HasSuffix(t, "*")})
	}
	var b strings.Builder
	fmt.Fprintf(&b, "TITLE: %s\r\nFCM: NON-DROP FRAME\r\n", strings.ToUpper(nz(j.Name, j.ID)))
	fmt.Fprintf(&b, "* GERADO POR RACK MONITOR BY DON V%s\r\n* REFERENCIA DE TC: %s (%.2f fps)\r\n* ATEM: %s M/E %d\r\n\r\n", AppVersion, strings.ToUpper(deckName), fps, atemID, me+1)
	n := 0
	for i, s := range segs {
		if !s.hasTC || s.recIn == "" { continue }
		in, ok := tcToFrames(s.recIn, base)
		if !ok { continue }
		var out int
		if i+1 < len(segs) && segs[i+1].hasTC {
			out, _ = tcToFrames(segs[i+1].recIn, base)
		} else {
			end := j.Closed
			if end.IsZero() { end = time.Now() }
			if i+1 < len(segs) { end = segs[i+1].at }
			out = in + int(end.Sub(s.at).Seconds()*fps)
		}
		if out <= in { continue }
		n++
		ti, to := framesToTC(in, base), framesToTC(out, base)
		fmt.Fprintf(&b, "%03d  %-8s V     C        %s %s %s %s\r\n* FROM CLIP NAME: %s\r\n\r\n", n, reelName(s.name), ti, to, ti, to, strings.ToUpper(s.name))
	}
	if len(markers) > 0 {
		b.WriteString("* MARCADORES DO OPERADOR\r\n")
		for _, m := range markers {
			tcs, _ := m.Data["tcs"].(map[string]any)
			t, _ := tcs[deckID].(string)
			lbl, _ := m.Data["label"].(string)
			fmt.Fprintf(&b, "* LOC: %s %s %s\r\n", nz(strings.TrimSuffix(t, "*"), "--:--:--:--"), m.At.Format("15:04:05"), lbl)
		}
	}
	return b.String(), n, nil
}
