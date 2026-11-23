package main

// Relógio/rundown, histórico entre jobs, fotos de patch (comparar montagem)
// e relatório automático no fim do job (Telegram/e-mail).

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/smtp"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ---------- rundown ----------

type RItem struct {
	Title string `json:"title"`
	Dur   int    `json:"dur"` // segundos planejados
}

type Rundown struct {
	Items     []RItem   `json:"items"`
	Current   int       `json:"current"` // -1 = não começou
	StartedAt time.Time `json:"startedAt"`
	BlockAt   time.Time `json:"blockAt"`
	Ended     time.Time `json:"ended"`
	Log       []struct {
		Index int       `json:"index"`
		At    time.Time `json:"at"`
	} `json:"log"`
}

func (h *Hub) rundownPath() string {
	h.mu.Lock(); defer h.mu.Unlock()
	if h.job == nil { return "" }
	d, _ := h.jobDir(h.job.ID)
	return filepath.Join(d, "rundown.json")
}

func (h *Hub) loadRundown() {
	p := h.rundownPath()
	r := &Rundown{Current: -1}
	if b, err := os.ReadFile(p); err == nil { _ = json.Unmarshal(b, r) }
	h.mu.Lock(); h.rundown = r; h.dirty = true; h.mu.Unlock()
}

func (h *Hub) saveRundown() {
	p := h.rundownPath()
	if p == "" { return }
	h.mu.Lock(); b, _ := json.MarshalIndent(h.rundown, "", "  "); h.mu.Unlock()
	_ = os.WriteFile(p, b, 0o644)
}

func (h *Hub) RundownAction(action, origin string, items []RItem) error {
	h.mu.Lock()
	r := h.rundown
	if r == nil { r = &Rundown{Current: -1}; h.rundown = r }
	now := time.Now()
	label := ""
	switch action {
	case "set":
		r.Items = items
		if r.Current >= len(items) { r.Current = len(items) - 1 }
	case "start":
		if len(r.Items) == 0 { r.Items = []RItem{{Title: "PROGRAMA", Dur: 0}} } // sem roteiro: cronômetro livre
		r.Current, r.StartedAt, r.BlockAt, r.Ended = 0, now, now, time.Time{}
		r.Log = nil
		label = "INÍCIO · " + r.Items[0].Title
	case "next", "prev":
		if r.Current < 0 { h.mu.Unlock(); return fmt.Errorf("o programa ainda não começou") }
		n := r.Current + 1
		if action == "prev" { n = r.Current - 1 }
		if n < 0 { n = 0 }
		if n >= len(r.Items) {
			r.Ended = now
			label = "FIM DO PROGRAMA"
			r.Current = len(r.Items)
		} else {
			r.Current, r.BlockAt = n, now
			label = "BLOCO · " + r.Items[n].Title
		}
	case "reset":
		r.Current, r.StartedAt, r.BlockAt, r.Ended, r.Log = -1, time.Time{}, time.Time{}, time.Time{}, nil
	default:
		h.mu.Unlock(); return fmt.Errorf("ação inválida")
	}
	if label != "" { r.Log = append(r.Log, struct {
		Index int       `json:"index"`
		At    time.Time `json:"at"`
	}{r.Current, now}) }
	h.dirty = true
	h.mu.Unlock()
	h.saveRundown()
	if label != "" { h.Marker(label, "", origin) }
	return nil
}

// PastRundown: roteiro de um job anterior, para copiar (eventos de vários dias).
type PastRundown struct {
	JobID   string    `json:"jobId"`
	JobName string    `json:"jobName"`
	Client  string    `json:"client"`
	Venue   string    `json:"venue"`
	Opened  time.Time `json:"opened"`
	Items   []RItem   `json:"items"`
	Total   int       `json:"total"` // segundos planejados
}

// roteiro sugerido que a tela grava sozinha num job novo: não vale a pena oferecer para copiar
func defaultRundown(items []RItem) bool {
	return len(items) == 3 && items[0] == RItem{"ABERTURA", 120} && items[1] == RItem{"BLOCO 1", 900} && items[2] == RItem{"INTERVALO", 300}
}

// PastRundowns: roteiros dos jobs anteriores (mais recentes primeiro), sem o job atual.
func (h *Hub) PastRundowns(limit int) []PastRundown {
	h.mu.Lock(); cur := ""; if h.job != nil { cur = h.job.ID }; h.mu.Unlock()
	out := []PastRundown{}
	for _, j := range h.ListJobs() {
		if j.ID == cur { continue }
		dir, err := h.jobDir(j.ID)
		if err != nil { continue }
		b, err := os.ReadFile(filepath.Join(dir, "rundown.json"))
		if err != nil { continue }
		var r Rundown
		if json.Unmarshal(b, &r) != nil || len(r.Items) == 0 || defaultRundown(r.Items) { continue }
		p := PastRundown{JobID: j.ID, JobName: j.Name, Client: j.Client, Venue: j.Venue, Opened: j.Opened, Items: r.Items}
		for _, it := range r.Items { p.Total += it.Dur }
		out = append(out, p)
		if len(out) >= limit { break }
	}
	return out
}

// ---------- histórico entre jobs ----------

func (h *Hub) History(days int) map[string]any {
	since := time.Now().AddDate(0, 0, -days)
	devsAgg := map[string]*struct {
		Name       string  `json:"name"`
		Drops      int     `json:"drops"`
		Alerts     int     `json:"alerts"`
		Jobs       int     `json:"jobs"`
		OffSeconds float64 `json:"offSeconds"`
	}{}
	decks := map[string]*struct {
		Name        string  `json:"name"`
		RecSeconds  float64 `json:"recSeconds"`
		Interrupted int     `json:"interrupted"`
		SlotChanges int     `json:"slotChanges"`
	}{}
	titles := map[string]int{}
	var jobs []map[string]any
	total, crit, cuts, markers := 0.0, 0, 0, 0
	for _, j := range h.ListJobs() {
		if j.Opened.Before(since) { continue }
		r, err := h.BuildReport(j.ID)
		if err != nil { continue }
		total += r.Seconds
		crit += r.Counts["crit"]
		jc := 0
		for _, m := range r.MEs { jc += m.Cuts }
		cuts += jc
		for _, e := range r.Timeline { if e.Type == "MARCADOR" { markers++ } }
		for k, v := range r.Titles { titles[k] += v }
		for _, d := range r.Devices {
			x := devsAgg[d.Device]
			if x == nil { x = &struct {
				Name       string  `json:"name"`
				Drops      int     `json:"drops"`
				Alerts     int     `json:"alerts"`
				Jobs       int     `json:"jobs"`
				OffSeconds float64 `json:"offSeconds"`
			}{Name: d.DevName}; devsAgg[d.Device] = x }
			x.Drops += d.Drops; x.Alerts += d.Alerts; x.OffSeconds += d.OffSeconds; x.Jobs++
		}
		for _, d := range r.Decks {
			x := decks[d.Device]
			if x == nil { x = &struct {
				Name        string  `json:"name"`
				RecSeconds  float64 `json:"recSeconds"`
				Interrupted int     `json:"interrupted"`
				SlotChanges int     `json:"slotChanges"`
			}{Name: d.DevName}; decks[d.Device] = x }
			x.RecSeconds += d.RecSeconds; x.Interrupted += d.Interrupted; x.SlotChanges += d.SlotChanges
		}
		jobs = append(jobs, map[string]any{"id": j.ID, "name": j.Name, "opened": j.Opened, "seconds": r.Seconds, "crit": r.Counts["crit"], "cuts": jc})
	}
	type kv struct{ K string `json:"title"`; V int `json:"count"` }
	var tl []kv
	for k, v := range titles { tl = append(tl, kv{k, v}) }
	sort.Slice(tl, func(a, b int) bool { return tl[a].V > tl[b].V })
	var dl []any
	keys := make([]string, 0, len(devsAgg))
	for k := range devsAgg { keys = append(keys, k) }
	sort.Slice(keys, func(a, b int) bool { x, y := devsAgg[keys[a]], devsAgg[keys[b]]; if x.Drops != y.Drops { return x.Drops > y.Drops }; return x.Alerts > y.Alerts })
	for _, k := range keys { dl = append(dl, devsAgg[k]) }
	var kl []any
	for _, v := range decks { kl = append(kl, v) }
	return map[string]any{"days": days, "jobs": jobs, "seconds": total, "crit": crit, "cuts": cuts, "markers": markers, "titles": tl, "devices": dl, "decks": kl}
}

// ---------- fotos do patch ----------

type PatchSnap struct {
	Name    string                    `json:"name"`
	At      time.Time                 `json:"at"`
	Devices map[string]map[string]any `json:"devices"`
}

func (h *Hub) patchDir() string { d := filepath.Join(h.paths.Root, "patch"); _ = os.MkdirAll(d, 0o755); return d }

func (h *Hub) currentPatch() map[string]map[string]string {
	h.mu.Lock(); defer h.mu.Unlock()
	out := map[string]map[string]string{}
	for id, s := range h.states {
		m := map[string]string{"_name": s.Name}
		if a := s.Atem; a != nil && s.Online {
			for k, in := range a.Inputs { m["Input "+strconv.Itoa(k)] = in.Long + " / " + in.Short }
			for k, src := range a.Aux { m[fmt.Sprintf("AUX %02d fonte", k+1)] = a.Name(src) }
		} else if hb := s.Hub; hb != nil && hb.Ready {
			for k, v := range hb.InLabels { m[fmt.Sprintf("Entrada %02d nome", k+1)] = v }
			for k, v := range hb.OutLabels { m[fmt.Sprintf("Saída %02d nome", k+1)] = v }
			for o, i := range hb.Routes { m[fmt.Sprintf("Saída %02d rota", o+1)] = fmt.Sprintf("%d %s", i+1, hb.InLabels[i]) }
		} else { continue }
		out[id] = m
	}
	return out
}

func (h *Hub) SavePatch(name string) (string, error) {
	cur := h.currentPatch()
	snap := map[string]any{"name": nz(name, "Patch"), "at": time.Now(), "devices": cur}
	b, _ := json.MarshalIndent(snap, "", " ")
	fn := "PATCH_" + time.Now().Format("20060102_150405") + "_" + slug(name) + ".json"
	return fn, os.WriteFile(filepath.Join(h.patchDir(), fn), b, 0o644)
}

func (h *Hub) ListPatches() []map[string]any {
	ents, _ := os.ReadDir(h.patchDir())
	out := []map[string]any{}
	for _, e := range ents {
		var p struct{ Name string; At time.Time }
		if b, err := os.ReadFile(filepath.Join(h.patchDir(), e.Name())); err == nil && json.Unmarshal(b, &p) == nil {
			out = append(out, map[string]any{"file": e.Name(), "name": p.Name, "at": p.At})
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a]["file"].(string) > out[b]["file"].(string) })
	return out
}

func (h *Hub) DiffPatch(file string) ([]map[string]string, string, error) {
	file = filepath.Base(file)
	b, err := os.ReadFile(filepath.Join(h.patchDir(), file))
	if err != nil { return nil, "", fmt.Errorf("foto não encontrada") }
	var old struct {
		Name    string                       `json:"name"`
		Devices map[string]map[string]string `json:"devices"`
	}
	if err := json.Unmarshal(b, &old); err != nil { return nil, "", err }
	cur := h.currentPatch()
	out := []map[string]string{}
	for id, om := range old.Devices {
		cm, ok := cur[id]
		if !ok { out = append(out, map[string]string{"device": om["_name"], "item": "equipamento", "before": "presente", "after": "ausente ou desconectado"}); continue }
		keys := map[string]bool{}
		for k := range om { keys[k] = true }
		for k := range cm { keys[k] = true }
		for k := range keys {
			if k == "_name" { continue }
			if om[k] != cm[k] { out = append(out, map[string]string{"device": cm["_name"], "item": k, "before": nz(om[k], "—"), "after": nz(cm[k], "—")}) }
		}
	}
	for id, cm := range cur { if _, ok := old.Devices[id]; !ok { out = append(out, map[string]string{"device": cm["_name"], "item": "equipamento", "before": "ausente", "after": "presente"}) } }
	sort.Slice(out, func(a, b int) bool { if out[a]["device"] != out[b]["device"] { return out[a]["device"] < out[b]["device"] }; return out[a]["item"] < out[b]["item"] })
	return out, old.Name, nil
}

// ---------- relatório automático no fim do job ----------

type EmailCfg struct {
	Enabled bool   `json:"enabled"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
	User    string `json:"user"`
	Pass    string `json:"pass"`
	From    string `json:"from"`
	To      string `json:"to"`
}

func (h *Hub) summaryText(r *Report) (string, string) {
	cuts, rec, drops, mk := 0, 0.0, 0, 0
	for _, m := range r.MEs { cuts += m.Cuts }
	for _, d := range r.Decks { rec += d.RecSeconds }
	for _, d := range r.Devices { drops += d.Drops }
	for _, e := range r.Timeline { if e.Type == "MARCADOR" { mk++ } }
	var t strings.Builder
	fmt.Fprintf(&t, "📋 FIM DO JOB: %s\n%s → %s (%s)\n\n", r.Job.Name, r.From.Format("02/01 15:04"), r.To.Format("15:04"), hms(int(r.Seconds)))
	fmt.Fprintf(&t, "✂️ Cortes no PGM: %d\n⏺ Tempo gravado (soma): %s\n🔌 Quedas de conexão: %d\n🔴 Eventos críticos: %d\n📍 Marcadores: %d\n", cuts, hms(int(rec)), drops, r.Counts["crit"], mk)
	for _, m := range r.MEs {
		if m.ME != 0 || len(m.Sources) == 0 { continue }
		fmt.Fprintf(&t, "\nMais tempo no ar (%s):\n", m.DevName)
		for i, s := range m.Sources { if i >= 3 { break }; fmt.Fprintf(&t, " %d. %s — %s\n", i+1, s.Name, hms(int(s.Seconds))) }
	}
	for _, d := range r.Decks { if d.Interrupted > 0 { fmt.Fprintf(&t, "\n⚠️ %s: %d interrupção(ões) de gravação", d.DevName, d.Interrupted) } }
	if len(r.Critical) > 0 {
		t.WriteString("\n\nOcorrências críticas:\n")
		for i, e := range r.Critical { if i >= 8 { fmt.Fprintf(&t, " + %d no relatório\n", len(r.Critical)-8); break }; fmt.Fprintf(&t, " %s %s — %s\n", e.At.Format("15:04"), e.DevName, e.Type) }
	}
	fmt.Fprintf(&t, "\n— RACK MONITOR BY DON · %s", r.Station)
	txt := t.String()
	html := "<div style=\"font-family:Arial,sans-serif;font-size:14px\"><h2 style=\"color:#c81424\">RACK MONITOR BY DON</h2><pre style=\"font-family:Arial,sans-serif;font-size:14px;white-space:pre-wrap\">" +
		strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(txt) + "</pre></div>"
	return txt, html
}

func sendEmail(c EmailCfg, subject, html string) error {
	if c.Host == "" || c.To == "" { return fmt.Errorf("configure servidor e destinatário") }
	if c.Port == 0 { c.Port = 587 }
	from := nz(c.From, c.User)
	to := []string{}
	for _, x := range strings.Split(c.To, ",") { if x = strings.TrimSpace(x); x != "" { to = append(to, x) } }
	msg := "From: " + from + "\r\nTo: " + strings.Join(to, ", ") + "\r\nSubject: =?UTF-8?B?" + b64(subject) + "?=\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n" + html
	addr := net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
	var auth smtp.Auth
	if c.User != "" { auth = smtp.PlainAuth("", c.User, c.Pass, c.Host) }
	if c.Port == 465 {
		conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: c.Host})
		if err != nil { return err }
		cl, err := smtp.NewClient(conn, c.Host)
		if err != nil { return err }
		defer cl.Close()
		if auth != nil { if err := cl.Auth(auth); err != nil { return err } }
		if err := cl.Mail(from); err != nil { return err }
		for _, t := range to { if err := cl.Rcpt(t); err != nil { return err } }
		w, err := cl.Data()
		if err != nil { return err }
		w.Write([]byte(msg)); w.Close()
		return cl.Quit()
	}
	return smtp.SendMail(addr, auth, from, to, []byte(msg))
}

func (h *Hub) notifyJobEnd(id string) {
	time.Sleep(500 * time.Millisecond)
	r, err := h.BuildReport(id)
	if err != nil { return }
	txt, html := h.summaryText(r)
	cfgMu.RLock()
	t, e, send := h.cfg.Telegram, h.cfg.Email, h.cfg.ReportOnClose
	cfgMu.RUnlock()
	if !send { return }
	if t.Enabled { if err := sendTelegram(t, txt); err != nil { h.Log("", "RACK MONITOR", "RELATÓRIO NÃO ENVIADO", "warn", "Telegram: "+err.Error(), nil) } }
	if e.Enabled { if err := sendEmail(e, "Relatório do job: "+r.Job.Name, html); err != nil { h.Log("", "RACK MONITOR", "RELATÓRIO NÃO ENVIADO", "warn", "E-mail: "+err.Error(), nil) } }
}

func b64(s string) string {
	const t = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	b := []byte(s)
	var out strings.Builder
	for i := 0; i < len(b); i += 3 {
		var n uint32
		k := min(3, len(b)-i)
		for j := 0; j < 3; j++ { n <<= 8; if j < k { n |= uint32(b[i+j]) } }
		for j := 0; j < 4; j++ { if j <= k { out.WriteByte(t[(n>>(18-6*j))&63]) } else { out.WriteByte('=') } }
	}
	return out.String()
}
