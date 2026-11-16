package main

// V2.9 — operação de gravação:
//   • nome automático dos clipes (REC pelo monitor grava "JOB_DECK_001")
//   • inventário de cartões (o que ficou em cada slot de cada HyperDeck)
//   • alarme "esqueceu de gravar"

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ---------------- nome automático ----------------

var nameSafe = regexp.MustCompile(`[^A-Za-z0-9_\-]+`)
var deckPrefix = regexp.MustCompile(`(?i)^\s*hyper\s*deck\s*[-_:·]*\s*`)

func safeTok(s string) string {
	s = strings.NewReplacer("á", "a", "à", "a", "ã", "a", "â", "a", "é", "e", "ê", "e", "í", "i", "ó", "o", "ô", "o", "õ", "o", "ú", "u", "ç", "c",
		"Á", "A", "À", "A", "Ã", "A", "Â", "A", "É", "E", "Ê", "E", "Í", "I", "Ó", "O", "Ô", "O", "Õ", "O", "Ú", "U", "Ç", "C").Replace(s)
	s = nameSafe.ReplaceAllString(strings.TrimSpace(s), "_")
	s = strings.Trim(regexp.MustCompile(`_+`).ReplaceAllString(s, "_"), "_")
	if len(s) > 40 { s = s[:40] }
	return s
}

// clipParts: devolve o nome antes e depois do contador {NNN}.
func (h *Hub) clipParts(id string) (pre, post string) {
	h.mu.Lock()
	tpl := h.cfg.ClipTemplate
	job := "SEM_JOB"
	if h.job != nil && strings.TrimSpace(h.job.Name) != "" { job = h.job.Name }
	dname := id
	if s := h.states[id]; s != nil { dname = s.Name }
	h.mu.Unlock()
	if !strings.Contains(tpl, "{NNN}") { tpl += "_{NNN}" }
	now := time.Now()
	r := strings.NewReplacer("{JOB}", safeTok(job), "{DECK}", safeTok(deckPrefix.ReplaceAllString(dname, "")),
		"{DATA}", now.Format("20060102"), "{HORA}", now.Format("1504"))
	i := strings.Index(tpl, "{NNN}")
	pre, post = r.Replace(tpl[:i]), r.Replace(tpl[i+5:])
	pre, post = nameSafe.ReplaceAllString(pre, "_"), nameSafe.ReplaceAllString(post, "_")
	return
}

var clipSeq sync.Map // deck|prefixo → último número usado

// nextClipNumber: maior número já existente nos cartões (com o mesmo prefixo) + 1.
func (h *Hub) nextClipNumber(id, pre string) int {
	key := id + "|" + pre
	if v, ok := clipSeq.Load(key); ok { return v.(int) + 1 }
	max := 0
	if slots, err := deckClips(h, id); err == nil {
		re := regexp.MustCompile("^" + regexp.QuoteMeta(pre) + `(\d+)`)
		for _, s := range slots {
			for _, c := range s.Clips {
				if m := re.FindStringSubmatch(c.Name); m != nil {
					if n, _ := strconv.Atoi(m[1]); n > max { max = n }
				}
			}
		}
	}
	clipSeq.Store(key, max)
	return max + 1
}

// PreviewClipName: o próximo nome, sem consumir o número.
func (h *Hub) PreviewClipName(id string) string {
	h.mu.Lock(); off := h.cfg.ClipNameOff; h.mu.Unlock()
	if off { return "" }
	pre, post := h.clipParts(id)
	return fmt.Sprintf("%s%03d%s", pre, h.nextClipNumber(id, pre), post)
}

// TakeClipName: gera e reserva o nome para um REC enviado agora.
func (h *Hub) TakeClipName(id string) string {
	h.mu.Lock(); off := h.cfg.ClipNameOff; h.mu.Unlock()
	if off { return "" }
	pre, post := h.clipParts(id)
	n := h.nextClipNumber(id, pre)
	clipSeq.Store(id+"|"+pre, n)
	return fmt.Sprintf("%s%03d%s", pre, n, post)
}

// nome enviado no último REC (para anotar no evento "REC INICIADO")
type sentName struct {
	name string
	at   time.Time
}

var lastSentName sync.Map

func noteSentName(id, name string) { if name != "" { lastSentName.Store(id, sentName{name, time.Now()}) } }
func takeSentName(id string) string {
	if v, ok := lastSentName.LoadAndDelete(id); ok {
		if s := v.(sentName); time.Since(s.at) < 20*time.Second { return s.name }
	}
	return ""
}

// ---------------- inventário de cartões ----------------

type InvSession struct {
	Start    time.Time `json:"start"`
	Stop     time.Time `json:"stop"`
	Seconds  int       `json:"seconds"`
	Slots    []int     `json:"slots"`
	Name     string    `json:"name"`
	Open     bool      `json:"open"`
	Initial  bool      `json:"initial"`
}

type InvClip struct {
	Clip
	Seconds  int       `json:"seconds"`
	RecStart time.Time `json:"recStart"`
	RecStop  time.Time `json:"recStop"`
}

type InvSlot struct {
	Slot    int       `json:"slot"`
	Volume  string    `json:"volume"`
	Status  string    `json:"status"`
	Clips   []InvClip `json:"clips"`
	Seconds int       `json:"seconds"`
	Error   string    `json:"error,omitempty"`
}

type InvDeck struct {
	ID       string       `json:"id"`
	Name     string       `json:"name"`
	Host     string       `json:"host"`
	Model    string       `json:"model"`
	Via      string       `json:"via"`
	Note     string       `json:"note"`
	Slots    []InvSlot    `json:"slots"`
	Sessions []InvSession `json:"sessions"`
	RecSec   int          `json:"recSec"`
}

type Inventory struct {
	Job       *Job      `json:"job"`
	Generated time.Time `json:"generated"`
	Live      bool      `json:"live"`
	Snapshot  bool      `json:"snapshot"` // lista de clipes lida dos cartões
	Decks     []InvDeck `json:"decks"`
	Version   string    `json:"version"`
}

func tcSeconds(tc string) int {
	p := strings.FieldsFunc(tc, func(r rune) bool { return r == ':' || r == ';' })
	if len(p) < 3 { return 0 }
	hh, _ := strconv.Atoi(p[0]); mm, _ := strconv.Atoi(p[1]); ss, _ := strconv.Atoi(p[2])
	return hh*3600 + mm*60 + ss
}

func (h *Hub) invPath(id string) string {
	d, err := h.jobDir(id)
	if err != nil { return "" }
	return filepath.Join(d, "inventario.json")
}

// BuildInventory: sessões de REC do job (pelo log) + lista de clipes dos cartões (ao vivo).
func (h *Hub) BuildInventory(jobID string, readCards bool) (*Inventory, error) {
	h.mu.Lock()
	cur := h.job != nil && h.job.ID == jobID
	var job *Job
	if cur { jj := *h.job; job = &jj }
	h.mu.Unlock()
	if !cur {
		// job encerrado: usa o inventário salvo no encerramento, se houver
		if b, err := os.ReadFile(h.invPath(jobID)); err == nil {
			var inv Inventory
			if json.Unmarshal(b, &inv) == nil { inv.Live = false; return &inv, nil }
		}
		j, err := h.readJob(jobID)
		if err != nil { return nil, err }
		job = j
		readCards = false
	}
	evs, err := h.ReadEvents(jobID)
	if err != nil { return nil, err }

	inv := &Inventory{Job: job, Generated: time.Now(), Live: cur, Snapshot: readCards, Decks: []InvDeck{}, Version: AppVersion}
	byID := map[string]*InvDeck{}
	order := []string{}
	known := map[string]bool{}
	h.mu.Lock()
	for _, d := range h.cfg.Devices {
		known[d.ID] = true
		if d.Kind != "hyperdeck" || !d.Enabled { continue }
		x := &InvDeck{ID: d.ID, Name: d.Name, Host: d.Host, Via: d.Via, Slots: []InvSlot{}, Sessions: []InvSession{}}
		if s := h.states[d.ID]; s != nil && s.Deck != nil { x.Model = s.Deck.Model }
		byID[d.ID] = x; order = append(order, d.ID)
	}
	h.mu.Unlock()

	// sessões de gravação a partir dos eventos
	open := map[string]*InvSession{}
	for _, e := range evs {
		x := byID[e.Device]
		if x == nil {
			if e.Device == "" || known[e.Device] || !strings.HasPrefix(e.Type, "REC ") { continue }
			x = &InvDeck{ID: e.Device, Name: e.DevName, Slots: []InvSlot{}, Sessions: []InvSession{}, Note: "não está mais na configuração"}
			byID[e.Device] = x; order = append(order, e.Device)
		}
		switch e.Type {
		case "REC INICIADO":
			if open[e.Device] != nil { continue }
			s := &InvSession{Start: e.At, Open: true}
			if e.Data != nil {
				if v, ok := e.Data["clipName"].(string); ok { s.Name = v }
				if v := toInt(e.Data["slot"]); v > 0 { s.Slots = []int{v} }
				if v, ok := e.Data["initial"].(bool); ok { s.Initial = v }
			}
			open[e.Device] = s
		case "TROCA DE SLOT":
			if s := open[e.Device]; s != nil && e.Data != nil {
				if v := toInt(e.Data["to"]); v > 0 {
					if len(s.Slots) == 0 { if f := toInt(e.Data["from"]); f > 0 { s.Slots = []int{f} } }
					s.Slots = append(s.Slots, v)
				}
			}
		case "REC PARADO", "REC INTERROMPIDO", "GRAVAÇÃO INTERROMPIDA":
			if s := open[e.Device]; s != nil {
				s.Stop, s.Open = e.At, false
				s.Seconds = int(s.Stop.Sub(s.Start).Seconds())
				x.Sessions = append(x.Sessions, *s)
				delete(open, e.Device)
			}
		}
	}
	for id, s := range open {
		s.Seconds = int(time.Since(s.Start).Seconds())
		if !cur { s.Seconds = 0 }
		byID[id].Sessions = append(byID[id].Sessions, *s)
	}

	// lista de clipes dos cartões
	for _, id := range order {
		x := byID[id]
		for _, s := range x.Sessions { x.RecSec += s.Seconds }
		if !readCards { continue }
		if x.Via == "companion" { x.Note = "Lido via Companion: a lista de clipes dos cartões não está disponível neste modo."; continue }
		h.mu.Lock(); online := h.states[id] != nil && h.states[id].Online; h.mu.Unlock()
		if !online { x.Note = "Fora do ar no momento — sem lista de clipes."; continue }
		sl, err := deckClips(h, id)
		if err != nil { x.Note = "Não foi possível ler os cartões: " + err.Error(); continue }
		for _, s := range sl {
			is := InvSlot{Slot: s.Slot, Volume: s.Volume, Status: s.Status, Error: s.Error, Clips: []InvClip{}}
			for _, c := range s.Clips {
				ic := InvClip{Clip: c, Seconds: tcSeconds(c.Duration)}
				for _, ss := range x.Sessions {
					if ss.Name != "" && strings.HasPrefix(c.Name, ss.Name) { ic.RecStart, ic.RecStop = ss.Start, ss.Stop; break }
				}
				is.Seconds += ic.Seconds
				is.Clips = append(is.Clips, ic)
			}
			x.Slots = append(x.Slots, is)
		}
	}
	for _, id := range order { inv.Decks = append(inv.Decks, *byID[id]) }
	return inv, nil
}

func (h *Hub) saveInventory(jobID string) {
	inv, err := h.BuildInventory(jobID, true)
	if err != nil { return }
	inv.Live = false
	b, _ := json.MarshalIndent(inv, "", " ")
	if p := h.invPath(jobID); p != "" { _ = os.WriteFile(p, b, 0o644) }
}

// ---------------- alarme "esqueceu de gravar" ----------------

type forgotState struct {
	start   time.Time
	why     string
	dev     string // ATEM que disparou ("" = relógio)
	clockAt time.Time
	black   map[string]bool
	seen    map[string]bool
}

// forgotRec roda dentro de evaluate() com h.mu travado.
func (h *Hub) forgotRec(now time.Time, r Rules, want map[string]cond) {
	f := &h.forgot
	if f.black == nil { f.black, f.seen = map[string]bool{}, map[string]bool{} }
	if r.ForgotRecOff { f.start = time.Time{}; return }

	// gatilho 1: relógio/rundown iniciado
	if rd := h.rundown; rd != nil {
		running := rd.Current >= 0 && !rd.StartedAt.IsZero() && rd.Ended.IsZero()
		if running && !rd.StartedAt.Equal(f.clockAt) {
			f.clockAt = rd.StartedAt
			if f.start.IsZero() { f.start, f.why, f.dev = rd.StartedAt, "O relógio/rundown foi iniciado às "+rd.StartedAt.Format("15:04:05"), "" }
		}
		if !running && f.dev == "" && !f.start.IsZero() { f.start = time.Time{} }
	}
	// gatilho 2: ATEM saiu do preto (M/E 1: FTB desligado e PGM diferente de Black)
	for _, d := range h.cfg.Devices {
		s := h.states[d.ID]
		if d.Kind != "atem" || s == nil || s.Atem == nil || !s.Atem.Ready || !s.Online { continue }
		m := s.Atem.MEs[0]
		if m == nil { continue }
		black := m.FTB || m.Program == 0
		if f.seen[d.ID] && f.black[d.ID] && !black && !r.ForgotRecNoATEM && f.start.IsZero() {
			f.start, f.dev = now, d.ID
			f.why = fmt.Sprintf("%s saiu do preto às %s (PGM: %s)", d.Name, now.Format("15:04:05"), s.Atem.Name(m.Program))
		}
		if black && f.dev == d.ID { f.start = time.Time{} } // voltou ao preto: cancela
		f.black[d.ID], f.seen[d.ID] = black, true
	}
	if f.start.IsZero() { return }

	// quais gravadores deveriam estar gravando
	ids := h.cfg.RecWatch
	if len(ids) == 0 {
		for _, d := range h.cfg.Devices { if d.Kind == "hyperdeck" && d.Enabled { ids = append(ids, d.ID) } }
	}
	var miss []string
	first := ""
	for _, id := range ids {
		s := h.states[id]
		if s == nil || !s.Enabled { continue }
		if s.Deck != nil && s.Deck.Recording() { continue }
		st := "fora do ar"
		if s.Online && s.Deck != nil { st = transportPT(s.Deck.Transport) }
		miss = append(miss, s.Name+" ("+st+")")
		if first == "" { first = id }
	}
	if len(miss) == 0 { f.start = time.Time{}; return } // todos gravando: missão cumprida
	if now.Sub(f.start) < time.Duration(r.ForgotRecSec)*time.Second { return }
	sort.Strings(miss)
	want["forgot"] = cond{first, "crit", "ESQUECEU DE GRAVAR?",
		fmt.Sprintf("%s, há %s, e não estão gravando: %s.", f.why, human(now.Sub(f.start)), strings.Join(miss, ", "))}
}
