package main

// Jobs (sessões de transmissão): cada job tem job.json + events.jsonl.
// O relatório é calculado a partir da linha do tempo gravada — nada é inventado.

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type Job struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Client   string    `json:"client"`
	Venue    string    `json:"venue"`
	Operator string    `json:"operator"`
	Notes    string    `json:"notes"`
	Opened   time.Time `json:"opened"`
	Closed   time.Time `json:"closed"`
}

type jobWriter struct {
	f *os.File
	w *bufio.Writer
	n int
}

func (j *jobWriter) write(e Event) {
	b, _ := json.Marshal(e)
	j.w.Write(b); j.w.WriteByte('\n')
	j.n++
	j.w.Flush() // cada evento vai para o disco imediatamente
}

func (j *jobWriter) close() { if j != nil { j.w.Flush(); j.f.Close() } }

var jobIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,80}$`)

func (h *Hub) jobDir(id string) (string, error) {
	if !jobIDRe.MatchString(id) { return "", fmt.Errorf("identificador de job inválido") }
	return filepath.Join(h.paths.Jobs, id), nil
}

func (h *Hub) saveJobMeta(j *Job) error {
	dir, err := h.jobDir(j.ID)
	if err != nil { return err }
	_ = os.MkdirAll(dir, 0o755)
	b, _ := json.MarshalIndent(j, "", "  ")
	return os.WriteFile(filepath.Join(dir, "job.json"), b, 0o644)
}

func (h *Hub) readJob(id string) (*Job, error) {
	dir, err := h.jobDir(id)
	if err != nil { return nil, err }
	b, err := os.ReadFile(filepath.Join(dir, "job.json"))
	if err != nil { return nil, err }
	var j Job
	if err := json.Unmarshal(b, &j); err != nil { return nil, err }
	return &j, nil
}

func (h *Hub) ListJobs() []*Job {
	ents, _ := os.ReadDir(h.paths.Jobs)
	out := []*Job{}
	for _, e := range ents {
		if !e.IsDir() { continue }
		if j, err := h.readJob(e.Name()); err == nil { out = append(out, j) }
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Opened.After(out[b].Opened) })
	return out
}

// attachJob abre (ou retoma) o job e o arquivo de eventos. Chamar sem h.mu.
func (h *Hub) attachJob(j *Job) error {
	dir, err := h.jobDir(j.ID)
	if err != nil { return err }
	_ = os.MkdirAll(dir, 0o755)
	f, err := os.OpenFile(filepath.Join(dir, "events.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil { return err }
	h.mu.Lock()
	if h.jobLog != nil { h.jobLog.close() }
	h.job = j
	h.jobLog = &jobWriter{f: f, w: bufio.NewWriter(f)}
	h.events = nil
	h.dirty = true
	h.mu.Unlock()
	cfgMu.Lock(); h.cfg.CurrentJob = j.ID; _ = saveConfig(h.paths, h.cfg); cfgMu.Unlock()
	h.loadRundown()
	return nil
}

func (h *Hub) StartupJob() {
	if id := h.cfg.CurrentJob; id != "" {
		if j, err := h.readJob(id); err == nil && j.Closed.IsZero() {
			if h.attachJob(j) == nil {
				// recarrega a linha do tempo do job para as telas (marcadores, checklist, alertas)
				if evs, err := h.ReadEvents(j.ID); err == nil {
					if len(evs) > 2000 { evs = evs[len(evs)-2000:] }
					h.mu.Lock(); h.events = evs; if n := len(evs); n > 0 { h.seq = evs[n-1].Seq }; h.mu.Unlock()
				}
				h.Log("", "RACK MONITOR", "JOB RETOMADO", "info", "Monitor reiniciado; linha do tempo continua no mesmo job.", nil)
				return
			}
		}
	}
	h.NewJob(Job{Name: "Job " + time.Now().Format("02/01/2006"), Operator: h.cfg.StationName}, true)
}

func (h *Hub) NewJob(meta Job, auto bool) *Job {
	h.CloseJob()
	now := time.Now()
	meta.ID = "JOB_" + now.Format("20060102_150405")
	// dois jobs no mesmo segundo não podem cair na mesma pasta (o segundo apagaria o primeiro)
	for n := 2; ; n++ {
		dir, err := h.jobDir(meta.ID)
		if err != nil { break }
		if _, err := os.Stat(dir); os.IsNotExist(err) { break }
		meta.ID = fmt.Sprintf("JOB_%s_%d", now.Format("20060102_150405"), n)
	}
	meta.Opened = now
	meta.Closed = time.Time{}
	if strings.TrimSpace(meta.Name) == "" { meta.Name = "Job " + now.Format("02/01/2006 15:04") }
	j := meta
	_ = h.saveJobMeta(&j)
	_ = h.attachJob(&j)
	msg := "Job aberto: " + j.Name
	if auto { msg = "Job automático aberto ao iniciar o monitor." }
	h.Log("", "RACK MONITOR", "JOB ABERTO", "info", msg, map[string]any{"client": j.Client, "venue": j.Venue, "operator": j.Operator})
	h.baseline()
	return &j
}

// baseline grava o estado de partida (PGM no ar, gravadores em REC) para o relatório.
func (h *Hub) baseline() {
	type item struct{ dev, name, typ, msg string; data map[string]any }
	var items []item
	h.mu.Lock()
	for _, d := range h.cfg.Devices {
		s := h.states[d.ID]
		if s == nil || !s.Online { continue }
		if s.Atem != nil {
			for me, m := range s.Atem.MEs {
				items = append(items, item{d.ID, d.Name, "PGM", fmt.Sprintf("M/E %d no ar: %s (estado inicial)", me+1, s.Atem.Name(m.Program)),
					map[string]any{"me": me, "src": m.Program, "name": s.Atem.Name(m.Program), "initial": true, "tcs": h.tcSnapshotLocked()}})
			}
		}
		if s.Deck != nil && s.Deck.Recording() {
			items = append(items, item{d.ID, d.Name, "REC INICIADO", "Já estava gravando na abertura do job.", map[string]any{"initial": true}})
		}
	}
	h.mu.Unlock()
	for _, it := range items { h.Log(it.dev, it.name, it.typ, "info", it.msg, it.data) }
}

func (h *Hub) UpdateJobMeta(meta Job) error {
	h.mu.Lock()
	j := h.job
	if j == nil { h.mu.Unlock(); return fmt.Errorf("nenhum job aberto") }
	j.Name, j.Client, j.Venue, j.Operator, j.Notes = meta.Name, meta.Client, meta.Venue, meta.Operator, meta.Notes
	h.dirty = true
	cp := *j
	h.mu.Unlock()
	return h.saveJobMeta(&cp)
}

func (h *Hub) CloseJob() {
	h.mu.Lock()
	j := h.job
	h.mu.Unlock()
	if j == nil { return }
	h.saveInventory(j.ID) // foto dos cartões antes de fechar (podem ser retirados depois)
	h.Log("", "RACK MONITOR", "JOB ENCERRADO", "info", "Job encerrado: "+j.Name, nil)
	h.mu.Lock()
	j.Closed = time.Now()
	if h.jobLog != nil { h.jobLog.close(); h.jobLog = nil }
	h.job = nil
	h.mu.Unlock()
	_ = h.saveJobMeta(j)
	go h.notifyJobEnd(j.ID)
}

func (h *Hub) ReadEvents(id string) ([]Event, error) {
	dir, err := h.jobDir(id)
	if err != nil { return nil, err }
	f, err := os.Open(filepath.Join(dir, "events.jsonl"))
	if err != nil { if os.IsNotExist(err) { return []Event{}, nil }; return nil, err }
	defer f.Close()
	out := []Event{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 4*1024*1024)
	for sc.Scan() {
		var e Event
		if json.Unmarshal(sc.Bytes(), &e) == nil { out = append(out, e) }
	}
	return out, nil
}

func (h *Hub) WriteCSV(id string, w io.Writer) error {
	evs, err := h.ReadEvents(id)
	if err != nil { return err }
	w.Write([]byte("\ufeff"))
	c := csv.NewWriter(w)
	c.Comma = ';'
	c.Write([]string{"data_hora", "equipamento", "evento", "severidade", "descricao", "origem"})
	for _, e := range evs { c.Write([]string{e.At.Format("2006-01-02 15:04:05.000"), e.DevName, e.Type, e.Sev, e.Msg, e.Origin}) }
	c.Flush()
	return c.Error()
}

// ---------- relatório ----------

type SourceTime struct {
	Name    string  `json:"name"`
	Seconds float64 `json:"seconds"`
	Takes   int     `json:"takes"`
}

type MEReport struct {
	Device  string       `json:"device"`
	DevName string       `json:"devName"`
	ME      int          `json:"me"`
	Cuts    int          `json:"cuts"`
	Used    int          `json:"used"`
	Sources []SourceTime `json:"sources"`
}

type DeckReport struct {
	Device      string  `json:"device"`
	DevName     string  `json:"devName"`
	RecSeconds  float64 `json:"recSeconds"`
	Starts      int     `json:"starts"`
	Stops       int     `json:"stops"`
	Interrupted int     `json:"interrupted"`
	SlotChanges int     `json:"slotChanges"`
}

type DevReport struct {
	Device     string  `json:"device"`
	DevName    string  `json:"devName"`
	Drops      int     `json:"drops"`
	OffSeconds float64 `json:"offSeconds"`
	Changes    int     `json:"changes"`
	Alerts     int     `json:"alerts"`
}

type CCUReport struct {
	Device  string         `json:"device"`
	DevName string         `json:"devName"`
	Camera  string         `json:"camera"`
	Count   int            `json:"count"`
	Params  map[string]int `json:"params"`
}

type Report struct {
	Job       *Job          `json:"job"`
	From      time.Time     `json:"from"`
	To        time.Time     `json:"to"`
	Seconds   float64       `json:"seconds"`
	Live      bool          `json:"live"`
	Counts    map[string]int `json:"counts"`
	MEs       []MEReport    `json:"mes"`
	Decks     []DeckReport  `json:"decks"`
	Devices   []DevReport   `json:"devices"`
	Critical  []Event       `json:"critical"`
	Titles    map[string]int `json:"titles"`
	CCU       []CCUReport   `json:"ccu"`
	Timeline  []Event       `json:"timeline"`
	Station   string        `json:"station"`
	Generated time.Time     `json:"generated"`
	Version   string        `json:"version"`
}

func (h *Hub) BuildReport(id string) (*Report, error) {
	j, err := h.readJob(id)
	if err != nil { return nil, err }
	h.mu.Lock()
	if h.job != nil && h.job.ID == id { cp := *h.job; j = &cp }
	h.mu.Unlock()
	evs, err := h.ReadEvents(id)
	if err != nil { return nil, err }
	end := j.Closed
	live := end.IsZero()
	if live { end = time.Now() }
	r := &Report{Titles: map[string]int{}, Job: j, From: j.Opened, To: end, Seconds: end.Sub(j.Opened).Seconds(), Live: live, Counts: map[string]int{},
		Station: h.cfg.StationName, Generated: time.Now(), Version: AppVersion}

	type meKey struct{ dev string; me int }
	type cur struct{ src int; name string; at time.Time }
	onAir := map[meKey]*cur{}
	acc := map[meKey]map[string]*SourceTime{}
	cuts := map[meKey]int{}
	names := map[string]string{}
	recAt := map[string]time.Time{}
	decks := map[string]*DeckReport{}
	devs := map[string]*DevReport{}
	offAt := map[string]time.Time{}
	ccu := map[string]*CCUReport{}
	dev := func(id, name string) *DevReport {
		if id == "" { return nil }
		d, ok := devs[id]
		if !ok { d = &DevReport{Device: id, DevName: name}; devs[id] = d }
		return d
	}
	deck := func(id, name string) *DeckReport {
		d, ok := decks[id]
		if !ok { d = &DeckReport{Device: id, DevName: name}; decks[id] = d }
		return d
	}
	closeSeg := func(k meKey, t time.Time) {
		c := onAir[k]
		if c == nil { return }
		m := acc[k]
		if m == nil { m = map[string]*SourceTime{}; acc[k] = m }
		st := m[c.name]
		if st == nil { st = &SourceTime{Name: c.name}; m[c.name] = st }
		st.Seconds += t.Sub(c.at).Seconds()
	}
	for _, e := range evs {
		if e.DevName != "" { names[e.Device] = e.DevName }
		r.Counts[e.Sev]++
		if e.Sev == "crit" { r.Critical = append(r.Critical, e) }
		if e.Sev != "trace" { r.Timeline = append(r.Timeline, e) }
		if e.Data != nil { if _, ok := e.Data["alert"]; ok && e.Type != "NORMALIZADO" { r.Titles[e.Type]++ } }
		if e.Data != nil { if _, ok := e.Data["alert"]; ok && e.Type != "NORMALIZADO" { if d := dev(e.Device, e.DevName); d != nil { d.Alerts++ } } }
		switch e.Type {
		case "PGM":
			me := toInt(e.Data["me"]); k := meKey{e.Device, me}
			name, _ := e.Data["name"].(string)
			src := toInt(e.Data["src"])
			if c := onAir[k]; c != nil && c.src == src { continue }
			closeSeg(k, e.At)
			if init, _ := e.Data["initial"].(bool); !init && onAir[k] != nil { cuts[k]++ }
			onAir[k] = &cur{src, name, e.At}
			m := acc[k]
			if m == nil { m = map[string]*SourceTime{}; acc[k] = m }
			if m[name] == nil { m[name] = &SourceTime{Name: name} }
			m[name].Takes++
		case "REC INICIADO":
			if _, ok := recAt[e.Device]; !ok { recAt[e.Device] = e.At }
			d := deck(e.Device, e.DevName)
			if init, _ := e.Data["initial"].(bool); !init { d.Starts++ }
		case "REC PARADO":
			d := deck(e.Device, e.DevName)
			d.Stops++
			if t, ok := recAt[e.Device]; ok { d.RecSeconds += e.At.Sub(t).Seconds(); delete(recAt, e.Device) }
		case "CONTROLE DE CÂMERA":
			cam := strings.SplitN(e.Msg, " · ", 2)[0]
			k := e.Device + "|" + cam
			cr := ccu[k]
			if cr == nil { cr = &CCUReport{Device: e.Device, DevName: e.DevName, Camera: cam, Params: map[string]int{}}; ccu[k] = cr }
			cr.Count++
			if p, _ := e.Data["param"].(string); p != "" { cr.Params[p]++ }
		case "TROCA DE SLOT":
			deck(e.Device, e.DevName).SlotChanges++
		case "GRAVAÇÃO INTERROMPIDA":
			deck(e.Device, e.DevName).Interrupted++
		case "DESCONECTADO":
			if d := dev(e.Device, e.DevName); d != nil { d.Drops++ }
			offAt[e.Device] = e.At
			// segmento de PGM termina quando a mesa cai
			for k := range onAir { if k.dev == e.Device { closeSeg(k, e.At); delete(onAir, k) } }
		case "CONECTADO":
			if t, ok := offAt[e.Device]; ok { if d := dev(e.Device, e.DevName); d != nil { d.OffSeconds += e.At.Sub(t).Seconds() }; delete(offAt, e.Device) }
		case "ROTA ALTERADA", "AUX ALTERADO":
			if d := dev(e.Device, e.DevName); d != nil { d.Changes++ }
		}
	}
	for k := range onAir { closeSeg(k, end) }
	for id, t := range recAt { decks[id].RecSeconds += end.Sub(t).Seconds() }
	for id, t := range offAt { if d := devs[id]; d != nil { d.OffSeconds += end.Sub(t).Seconds() } }
	for k, m := range acc {
		mr := MEReport{Device: k.dev, DevName: names[k.dev], ME: k.me, Cuts: cuts[k]}
		for _, st := range m { mr.Sources = append(mr.Sources, *st) }
		sort.Slice(mr.Sources, func(a, b int) bool { return mr.Sources[a].Seconds > mr.Sources[b].Seconds })
		mr.Used = len(mr.Sources)
		r.MEs = append(r.MEs, mr)
	}
	sort.Slice(r.MEs, func(a, b int) bool { if r.MEs[a].DevName != r.MEs[b].DevName { return r.MEs[a].DevName < r.MEs[b].DevName }; return r.MEs[a].ME < r.MEs[b].ME })
	for _, d := range decks { r.Decks = append(r.Decks, *d) }
	sort.Slice(r.Decks, func(a, b int) bool { return r.Decks[a].DevName < r.Decks[b].DevName })
	for _, d := range devs { r.Devices = append(r.Devices, *d) }
	sort.Slice(r.Devices, func(a, b int) bool { return r.Devices[a].DevName < r.Devices[b].DevName })
	for _, c := range ccu { r.CCU = append(r.CCU, *c) }
	sort.Slice(r.CCU, func(a, b int) bool { return r.CCU[a].Count > r.CCU[b].Count })
	if r.MEs == nil { r.MEs = []MEReport{} }
	if r.Decks == nil { r.Decks = []DeckReport{} }
	if r.Devices == nil { r.Devices = []DevReport{} }
	if r.Critical == nil { r.Critical = []Event{} }
	if r.Timeline == nil { r.Timeline = []Event{} }
	if r.CCU == nil { r.CCU = []CCUReport{} }
	if len(r.Timeline) > 4000 { r.Timeline = r.Timeline[len(r.Timeline)-4000:] }
	return r, nil
}

func toInt(v any) int {
	switch x := v.(type) {
	case float64: return int(x)
	case int: return x
	case int64: return int(x)
	}
	return 0
}

// WriteSummaryCSV: resumo enxuto — uma linha por fonte por M/E (vezes no ar, tempo, %).
func (h *Hub) WriteSummaryCSV(id string, w io.Writer) error {
	r, err := h.BuildReport(id)
	if err != nil { return err }
	w.Write([]byte("\ufeff"))
	c := csv.NewWriter(w)
	c.Comma = ';'
	c.Write([]string{"job", r.Job.Name, "inicio", r.From.Format("02/01/2006 15:04"), "duracao", hms(int(r.Seconds))})
	c.Write([]string{"atem", "me", "fonte", "vezes_no_ar", "tempo_no_ar", "percentual"})
	for _, m := range r.MEs {
		tot := 0.0
		for _, s := range m.Sources { tot += s.Seconds }
		if tot == 0 { tot = 1 }
		for _, s := range m.Sources {
			c.Write([]string{m.DevName, fmt.Sprintf("M/E %d", m.ME+1), s.Name, fmt.Sprint(s.Takes), hms(int(s.Seconds)), fmt.Sprintf("%.1f%%", s.Seconds/tot*100)})
		}
	}
	c.Flush()
	return c.Error()
}
