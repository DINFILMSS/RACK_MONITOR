package main

// Hub: estado central de todos os equipamentos, linha do tempo, alertas e
// distribuição em tempo real (Server-Sent Events) para todas as telas abertas.

import (
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

type AtemInput struct {
	ID    int    `json:"id"`
	Long  string `json:"long"`
	Short string `json:"short"`
	Port  int    `json:"port"`
}

type AtemME struct {
	Program      int          `json:"program"`
	Preview      int          `json:"preview"`
	InTransition bool         `json:"inTransition"`
	TransPos     int          `json:"transPos"`
	FTB          bool         `json:"ftb"`
	FTBTrans     bool         `json:"ftbTrans"`
	Keys         map[int]bool `json:"keys"`
	ProgramSince time.Time    `json:"programSince"`
}

type AtemState struct {
	Product      string             `json:"product"`
	Version      string             `json:"version"`
	Ready        bool               `json:"ready"`
	Inputs       map[int]*AtemInput `json:"inputs"`
	MEs          map[int]*AtemME    `json:"mes"`
	Aux          map[int]int        `json:"aux"`
	DSK          map[int]bool       `json:"dsk"`
	Tally        map[int]int        `json:"tally"`
	Stream       string             `json:"stream"`
	StreamTime   string             `json:"streamTime"`
	Rec          string             `json:"rec"`
	RecTime      string             `json:"recTime"`
	MacroRunning int                `json:"macroRunning"`
	Macros       map[int]string     `json:"macros"`
	Cams         map[int]*CamState  `json:"cams"`
	Warning      string             `json:"warning"`
	PacketsIn    int64              `json:"packetsIn"`
}

func newAtemState() *AtemState {
	return &AtemState{Inputs: map[int]*AtemInput{}, MEs: map[int]*AtemME{}, Aux: map[int]int{}, DSK: map[int]bool{},
		Tally: map[int]int{}, Macros: map[int]string{}, Cams: map[int]*CamState{}, MacroRunning: -1}
}

var auxDefaultRe = regexp.MustCompile(`(?i)^(auxiliary|auxiliar|aux|output|saída|saida)\s*\d+$`)

// AuxName: nome da saída AUX como está na mesa (ATEM Software Control → Output labels).
func (a *AtemState) AuxName(i int) string {
	if a == nil { return "" }
	if in, ok := a.Inputs[8001+i]; ok && in.Long != "" && !auxDefaultRe.MatchString(strings.TrimSpace(in.Long)) { return in.Long }
	return ""
}

// auxTitle: "AUX 2 (VIDEO MAN)" — rótulo do monitor tem prioridade, depois o nome da mesa.
func auxTitle(a *AtemState, i int, s AuxSetting) string {
	if s.Label == "" { s.Label = a.AuxName(i) }
	return auxLabel(i, s)
}

func (a *AtemState) Name(src int) string {
	if in, ok := a.Inputs[src]; ok && in.Long != "" { return in.Long }
	return fmt.Sprintf("Fonte %d", src)
}

type DeckSlot struct {
	Status      string `json:"status"`
	Volume      string `json:"volume"`
	RemainSec   int    `json:"remainSec"`
	RemainKnown bool   `json:"remainKnown"`
	VideoFormat string `json:"videoFormat"`
}

type DeckState struct {
	Model        string            `json:"model"`
	Protocol     string            `json:"protocol"`
	Software     string            `json:"software"`
	SlotCount    int               `json:"slotCount"`
	Transport    string            `json:"transport"`
	Speed        string            `json:"speed"`
	ActiveSlot   int               `json:"activeSlot"`
	Clip         string            `json:"clip"`
	TC           string            `json:"tc"`
	VideoFormat  string            `json:"videoFormat"`
	FileFormat   string            `json:"fileFormat"`
	Slots        map[int]*DeckSlot `json:"slots"`
	RecSince     time.Time         `json:"recSince"`
	RecSinceConn bool              `json:"recSinceConn"`
	TCStall      bool              `json:"tcStall"`
	TotalRemain  int               `json:"totalRemain"`
	lastTC       string
	tcAt         time.Time
	lastTCChange time.Time
}

func (d *DeckState) Recording() bool { return d != nil && d.Transport == "record" }

type HubState struct {
	Model     string         `json:"model"`
	Friendly  string         `json:"friendly"`
	Inputs    int            `json:"inputs"`
	Outputs   int            `json:"outputs"`
	InLabels  map[int]string `json:"inLabels"`
	OutLabels map[int]string `json:"outLabels"`
	Routes    map[int]int    `json:"routes"`
	Locks     map[int]string `json:"locks"`
	Ready     bool           `json:"ready"`
	Changes   int            `json:"changes"`
}

type ProbeState struct {
	OK      bool   `json:"ok"`
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type DevState struct {
	Device
	Status     string      `json:"status"`
	Online     bool        `json:"online"`
	Detail     string      `json:"detail"`
	Since      time.Time   `json:"since"`
	LastSeen   time.Time   `json:"lastSeen"`
	LatencyMs  float64     `json:"latencyMs"`
	Reconnects int         `json:"reconnects"`
	Severity   string      `json:"severity"`
	Atem       *AtemState  `json:"atem,omitempty"`
	Deck       *DeckState  `json:"deck,omitempty"`
	Hub        *HubState   `json:"hub,omitempty"`
	Probe      *ProbeState `json:"probe,omitempty"`
	Stream     *StreamState `json:"stream,omitempty"`
	Snmp       *SnmpState   `json:"snmp,omitempty"`
}

type Event struct {
	Seq     int64          `json:"seq"`
	At      time.Time      `json:"at"`
	Device  string         `json:"device"`
	DevName string         `json:"devName"`
	Type    string         `json:"type"`
	Sev     string         `json:"sev"` // trace | info | warn | crit
	Msg     string         `json:"msg"`
	Origin  string         `json:"origin,omitempty"` // quem gerou: IP/nome da estação, "externo", "regra"
	Data    map[string]any `json:"data,omitempty"`
}

type Alert struct {
	Key     string    `json:"key"`
	Device  string    `json:"device"`
	DevName string    `json:"devName"`
	Sev     string    `json:"sev"`
	Title   string    `json:"title"`
	Msg     string    `json:"msg"`
	Since   time.Time `json:"since"`
	Acked   bool      `json:"acked"`
	Latched bool      `json:"latched"`
	Active  bool      `json:"active"`
}

type Hub struct {
	mu          sync.Mutex
	cfg         *Config
	paths       Paths
	states      map[string]*DevState
	alerts      map[string]*Alert
	events      []Event
	seq         int64
	subs        map[chan []byte]struct{}
	dirty       bool
	job         *Job
	jobLog      *jobWriter
	recArmed    bool
	recArmedAt  time.Time
	ctrlUntil   time.Time
	commanded   map[string]time.Time // último comando enviado por esta estação (evita falso alarme)
	commandedBy map[string]string
	expect      map[string]expEntry
	pairSince   map[string]time.Time
	prevRec     map[string]bool
	prevStream  map[string]string
	rundown     *Rundown
	drivers     map[string]func()
	driverSig   map[string]string
	started     time.Time
	demo        bool
	forgot      forgotState
	updNotified string
}

func newHub(cfg *Config, p Paths, demo bool) *Hub {
	h := &Hub{cfg: cfg, paths: p, states: map[string]*DevState{}, alerts: map[string]*Alert{}, subs: map[chan []byte]struct{}{},
		commanded: map[string]time.Time{}, commandedBy: map[string]string{}, expect: map[string]expEntry{}, pairSince: map[string]time.Time{}, prevRec: map[string]bool{}, prevStream: map[string]string{},
		drivers: map[string]func(){}, driverSig: map[string]string{}, started: time.Now(), demo: demo}
	return h
}

// ---------- estado ----------

func (h *Hub) ensureState(d Device) *DevState {
	s, ok := h.states[d.ID]
	if !ok {
		s = &DevState{Device: d, Status: "CONECTANDO", Since: time.Now()}
		h.states[d.ID] = s
	}
	s.Device = d
	if !d.Enabled { s.Status = "DESATIVADO"; s.Online = false }
	return s
}

// Update executa fn com o estado do equipamento travado e agenda a difusão.
func (h *Hub) Update(id string, fn func(s *DevState)) {
	h.mu.Lock()
	if s, ok := h.states[id]; ok { fn(s); h.dirty = true }
	h.mu.Unlock()
}

func (h *Hub) SetOnline(id string, online bool, detail string) {
	h.mu.Lock()
	s, ok := h.states[id]
	if !ok { h.mu.Unlock(); return }
	was := s.Online
	s.Detail = detail
	if online {
		s.LastSeen = time.Now()
		if !was { s.Online = true; s.Status = "ONLINE"; s.Since = time.Now() }
	} else if was || s.Status == "CONECTANDO" {
		if was { s.Reconnects++ }
		s.Online = false; s.Status = "OFFLINE"
		if was { s.Since = time.Now() }
	}
	h.dirty = true
	name := s.Name
	h.mu.Unlock()
	if online && !was {
		h.Log(id, name, "CONECTADO", "info", detail, nil)
	} else if !online && was {
		h.Log(id, name, "DESCONECTADO", "crit", detail, nil)
	}
}

func (h *Hub) Commanded(id string) { h.CommandedBy(id, "") }

func (h *Hub) CommandedBy(id, origin string) {
	h.mu.Lock(); h.commanded[id] = time.Now(); h.commandedBy[id] = origin; h.mu.Unlock()
}

type expEntry struct {
	origin string
	at     time.Time
}

// Expect: registra a mudança EXATA que um comando deste monitor vai causar
// (ex.: "matriz:route:6:3"); só essa mudança herda a origem do comando.
func (h *Hub) Expect(key, origin string) { h.mu.Lock(); h.expect[key] = expEntry{origin, time.Now()}; h.mu.Unlock() }

func (h *Hub) originExact(key string) string {
	h.mu.Lock(); defer h.mu.Unlock()
	if e, ok := h.expect[key]; ok && time.Since(e.at) < 10*time.Second { delete(h.expect, key); return e.origin }
	return "externo"
}

// originFor: quem provavelmente causou a mudança observada no equipamento.
// Os protocolos Blackmagic NÃO informam qual cliente fez a troca; só sabemos
// com certeza quando o comando saiu deste monitor.
func (h *Hub) originFor(id string) string {
	h.mu.Lock(); defer h.mu.Unlock()
	if h.recentlyCommanded(id) { if o := h.commandedBy[id]; o != "" { return o }; return "este monitor" }
	return "externo"
}

func (h *Hub) recentlyCommanded(id string) bool {
	t, ok := h.commanded[id]
	return ok && time.Since(t) < 8*time.Second
}

// ---------- linha do tempo ----------

func (h *Hub) Log(dev, devName, typ, sev, msg string, data map[string]any) {
	h.LogO("", dev, devName, typ, sev, msg, data)
}

// LogO registra o evento com a origem (quem gerou).
func (h *Hub) LogO(origin, dev, devName, typ, sev, msg string, data map[string]any) {
	h.mu.Lock()
	h.seq++
	if devName == "" && dev != "" { if s, ok := h.states[dev]; ok { devName = s.Name } }
	e := Event{Seq: h.seq, At: time.Now(), Device: dev, DevName: devName, Type: typ, Sev: sev, Msg: msg, Origin: origin, Data: data}
	h.events = append(h.events, e)
	if len(h.events) > 3000 { h.events = append([]Event(nil), h.events[len(h.events)-2000:]...) }
	if h.jobLog != nil { h.jobLog.write(e) }
	b, _ := json.Marshal(e)
	h.broadcast("log", b)
	h.mu.Unlock()
	if sev == "crit" || sev == "warn" { log.Printf("[%s] %s · %s · %s", strings.ToUpper(sev), devName, typ, msg) }
}

func (h *Hub) RecentEvents(n int, minSev string) []Event {
	h.mu.Lock(); defer h.mu.Unlock()
	out := []Event{}
	for i := len(h.events) - 1; i >= 0 && len(out) < n; i-- {
		if sevRank(h.events[i].Sev) >= sevRank(minSev) { out = append(out, h.events[i]) }
	}
	return out
}

func sevRank(s string) int {
	switch s { case "trace": return 0; case "info": return 1; case "warn": return 2; case "crit": return 3 }
	return 1
}

// ---------- alertas ----------

type cond struct{ dev, sev, title, msg string }

// Latch cria um alerta que permanece até o operador reconhecer (ex.: gravação interrompida).
func (h *Hub) Latch(key, dev, sev, title, msg string) {
	h.mu.Lock()
	name := ""
	if s, ok := h.states[dev]; ok { name = s.Name }
	a, ok := h.alerts[key]
	if !ok {
		a = &Alert{Key: key, Device: dev, DevName: name, Since: time.Now()}
		h.alerts[key] = a
	}
	a.Sev, a.Title, a.Msg, a.Latched, a.Active, a.Acked = sev, title, msg, true, false, false
	h.dirty = true
	h.mu.Unlock()
	h.Log(dev, name, title, sev, msg, map[string]any{"alert": key})
	go h.webhook(key, sev, title, name, msg)
}

func (h *Hub) Ack(key string) {
	h.mu.Lock(); defer h.mu.Unlock()
	for k, a := range h.alerts {
		if key != "*" && k != key { continue }
		a.Acked = true
		if !a.Active { delete(h.alerts, k) }
	}
	h.dirty = true
}

func (h *Hub) evaluate() {
	now := time.Now()
	r := h.cfg.Rules
	want := map[string]cond{}
	type pend struct{ key, dev, sev, title, name, msg string }
	var raised []pend
	var latches []pend

	h.mu.Lock()
	watch := map[string]bool{}
	for _, id := range h.cfg.RecWatch { watch[id] = true }
	for _, d := range h.cfg.Devices {
		s := h.states[d.ID]
		if s == nil || !d.Enabled { continue }
		if !s.Online {
			if now.Sub(s.Since) > time.Duration(r.OfflineGraceSec)*time.Second {
				addr := d.Host
				if d.Port > 0 && d.Kind != "ping" { addr = fmt.Sprintf("%s:%d", d.Host, d.Port) }
				want["off:"+d.ID] = cond{d.ID, "crit", "SEM CONEXÃO", fmt.Sprintf("%s não responde há %s (%s).", addr, human(now.Sub(s.Since)), nz(s.Detail, "sem detalhe"))}
			}
			if watch[d.ID] && h.recArmed {
				want["recw:"+d.ID] = cond{d.ID, "crit", "GRAVAÇÃO NÃO CONFIRMADA", d.Name + " está fora do ar com a vigilância de gravação armada."}
			}
			continue
		}
		if s.Probe != nil && r.LatencyWarnMs > 0 && s.LatencyMs > float64(r.LatencyWarnMs) {
			want["lat:"+d.ID] = cond{d.ID, "warn", "LATÊNCIA ALTA", fmt.Sprintf("%.0f ms (limite %d ms)", s.LatencyMs, r.LatencyWarnMs)}
		}
		if s.Probe != nil && !s.Probe.OK {
			want["probe:"+d.ID] = cond{d.ID, "warn", "RESPOSTA INESPERADA", s.Probe.Message}
		}
		if sn := s.Snmp; sn != nil {
			if u := sn.UPS; u != nil {
				if u.OnBattery {
					want["ups:"+d.ID] = cond{d.ID, "crit", "NOBREAK NA BATERIA", fmt.Sprintf("Sem energia da rede há %s · carga %d%% · autonomia estimada %d min.", hms(u.SecOnBattery), u.Charge, u.MinutesLeft)}
				} else if u.BatteryLow {
					want["ups:"+d.ID] = cond{d.ID, "crit", "BATERIA DO NOBREAK FRACA", fmt.Sprintf("Carga %d%%.", u.Charge)}
				} else if u.Load >= 90 {
					want["ups:"+d.ID] = cond{d.ID, "warn", "NOBREAK SOBRECARREGADO", fmt.Sprintf("Carga de saída em %d%%.", u.Load)}
				}
			}
			for _, p := range sn.Ports {
				if p.Known && !p.Up {
					want[fmt.Sprintf("port:%s:%d", d.ID, p.Index)] = cond{d.ID, "crit", "PORTA DO SWITCH CAIU", fmt.Sprintf("Porta %d (%s) sem link.", p.Index, nz(p.Alias, p.Name))}
				}
			}
		}
		if st := s.Stream; st != nil {
			prev := h.prevStream[d.ID]
			if prev == "Streaming" && st.Status != "Streaming" && st.Status != "" && !h.recentlyCommanded(d.ID) {
				latches = append(latches, pend{"stream:" + d.ID, d.ID, "crit", "STREAM CAIU", d.Name, "Encoder saiu do ar (agora: " + st.Status + ")."})
			}
			h.prevStream[d.ID] = st.Status
			if st.Status == "Streaming" && st.CacheUsed >= 50 {
				want["cache:"+d.ID] = cond{d.ID, "warn", "INTERNET LENTA NO STREAM", fmt.Sprintf("Cache do encoder em %d%% — a conexão não está dando conta do bitrate.", st.CacheUsed)}
			}
		}
		if dk := s.Deck; dk != nil {
			rec := dk.Recording()
			if prev, ok := h.prevRec[d.ID]; ok && prev && !rec && !h.recentlyCommanded(d.ID) {
				sev := "warn"
				if h.recArmed && watch[d.ID] { sev = "crit" }
				latches = append(latches, pend{"stop:" + d.ID, d.ID, sev, "GRAVAÇÃO INTERROMPIDA", d.Name, "O gravador saiu de REC sem comando desta estação (transporte agora: " + transportPT(dk.Transport) + ")."})
			}
			h.prevRec[d.ID] = rec
			if sl := dk.Slots[dk.ActiveSlot]; sl != nil {
				switch sl.Status {
				case "empty": want["media:"+d.ID] = cond{d.ID, "crit", "SEM MÍDIA", fmt.Sprintf("Slot %d sem mídia.", dk.ActiveSlot)}
				case "error": want["media:"+d.ID] = cond{d.ID, "crit", "MÍDIA COM ERRO", fmt.Sprintf("Slot %d informou erro.", dk.ActiveSlot)}
				}
			}
			known := false
			for _, sl := range dk.Slots { if sl.RemainKnown && sl.Status == "mounted" { known = true } }
			if known {
				min := dk.TotalRemain / 60
				if min < r.RemainCritMin {
					want["space:"+d.ID] = cond{d.ID, "crit", "MÍDIA ACABANDO", fmt.Sprintf("Restam %s de gravação somando os slots.", hms(dk.TotalRemain))}
				} else if min < r.RemainWarnMin {
					want["space:"+d.ID] = cond{d.ID, "warn", "POUCO ESPAÇO", fmt.Sprintf("Restam %s de gravação somando os slots.", hms(dk.TotalRemain))}
				}
			}
			if sl := dk.Slots[dk.ActiveSlot]; rec && r.SlotWarnMin > 0 && sl != nil && sl.RemainKnown && sl.RemainSec < r.SlotWarnMin*60 {
				next, nsl := 0, (*DeckSlot)(nil)
				for i := 1; i <= 8; i++ {
					if x := dk.Slots[i]; i != dk.ActiveSlot && x != nil && x.Status == "mounted" && x.RemainSec > 60 { next, nsl = i, x; break }
				}
				if next > 0 {
					want["slotsw:"+d.ID] = cond{d.ID, "warn", "VAI TROCAR DE SLOT",
						fmt.Sprintf("Slot %d acaba em %s — a gravação deve continuar no slot %d (%s, %s livres).", dk.ActiveSlot, hms(sl.RemainSec), next, nz(nsl.Volume, "sem nome"), hms(nsl.RemainSec))}
				}
			}
			if dk.TCStall {
				want["tc:"+d.ID] = cond{d.ID, "crit", "REC SEM AVANÇO DE TIMECODE", "Transporte em REC mas o timecode parou em " + dk.TC + "."}
			}
			if h.recArmed && watch[d.ID] && !rec {
				want["recw:"+d.ID] = cond{d.ID, "crit", "NÃO ESTÁ GRAVANDO", "Vigilância armada: este gravador deveria estar em REC (agora: " + transportPT(dk.Transport) + ")."}
			}
		}
		if at := s.Atem; at != nil {
			for me, m := range at.MEs {
				if m.FTB && r.AlertFTB {
					want[fmt.Sprintf("ftb:%s:%d", d.ID, me)] = cond{d.ID, "warn", "FADE TO BLACK ATIVO", fmt.Sprintf("M/E %d está em preto (FTB).", me+1)}
				}
			}
			for _, ax := range h.cfg.AuxCfg {
				if ax.Device != d.ID || ax.Mode != "lock" || ax.Expected < 0 { continue }
				if cur, ok := at.Aux[ax.Aux]; ok && cur != ax.Expected {
					want[fmt.Sprintf("aux:%s:%d", d.ID, ax.Aux)] = cond{d.ID, "crit", "AUX FORA DO PADRÃO",
						fmt.Sprintf("%s está em %s; esperado %s.", auxTitle(at, ax.Aux, ax), at.Name(cur), at.Name(ax.Expected))}
				}
			}
			prev := h.prevStream[d.ID]
			if prev == "STREAMING" && at.Stream != "STREAMING" && at.Stream != "" {
				sev := "warn"
				if r.StreamDropIsCrit { sev = "crit" }
				if !h.recentlyCommanded(d.ID) { latches = append(latches, pend{"stream:" + d.ID, d.ID, sev, "STREAM CAIU", d.Name, "Streaming da ATEM saiu do ar (agora: " + at.Stream + ")."}) }
			}
			h.prevStream[d.ID] = at.Stream
		}
		if hb := s.Hub; hb != nil && hb.Ready {
			for _, pr := range h.cfg.Protected {
				if pr.Device != d.ID || pr.Expected < 0 { continue }
				if cur, ok := hb.Routes[pr.Output]; ok && cur != pr.Expected {
					want[fmt.Sprintf("route:%s:%d", d.ID, pr.Output)] = cond{d.ID, "crit", "ROTA PROTEGIDA FORA DO PADRÃO",
						fmt.Sprintf("Saída %d (%s) recebe %d (%s); esperado %d (%s).", pr.Output+1, hb.OutLabels[pr.Output], cur+1, hb.InLabels[cur], pr.Expected+1, hb.InLabels[pr.Expected])}
				}
			}
		}
	}
	// pares principal/backup
	for i, p := range h.cfg.Pairs {
		a, b := h.states[p.Primary], h.states[p.Backup]
		key := fmt.Sprintf("pair:%d", i)
		if a == nil || b == nil || a.Deck == nil || b.Deck == nil || !a.Online || !b.Online { delete(h.pairSince, key); continue }
		if a.Deck.Recording() != b.Deck.Recording() {
			if _, ok := h.pairSince[key]; !ok { h.pairSince[key] = now }
			if now.Sub(h.pairSince[key]) >= time.Duration(r.PairMismatchSec)*time.Second {
				sev := "warn"
				if h.recArmed { sev = "crit" }
				on, off := a, b
				if !a.Deck.Recording() { on, off = b, a }
				want[key] = cond{p.Primary, sev, "PAR DIVERGENTE", fmt.Sprintf("%s grava e %s não (%s).", on.Name, off.Name, nz(p.Label, "principal/backup"))}
			}
		} else { delete(h.pairSince, key) }
		if a.Deck.VideoFormat != "" && b.Deck.VideoFormat != "" && (a.Deck.VideoFormat != b.Deck.VideoFormat || (a.Deck.FileFormat != "" && b.Deck.FileFormat != "" && a.Deck.FileFormat != b.Deck.FileFormat)) {
			sev := "warn"
			if r.FormatMismatchCrit { sev = "crit" }
			want[key+":fmt"] = cond{p.Primary, sev, "FORMATOS DIFERENTES NO PAR", fmt.Sprintf("%s: %s %s · %s: %s %s", a.Name, a.Deck.VideoFormat, a.Deck.FileFormat, b.Name, b.Deck.VideoFormat, b.Deck.FileFormat)}
		}
	}
	h.forgotRec(now, r, want)
	// aplica
	for k, c := range want {
		a, ok := h.alerts[k]
		name := ""
		if s := h.states[c.dev]; s != nil { name = s.Name }
		if !ok {
			a = &Alert{Key: k, Device: c.dev, DevName: name, Since: now, Sev: c.sev, Title: c.title}
			h.alerts[k] = a
			raised = append(raised, pend{k, c.dev, c.sev, c.title, name, c.msg})
		} else if sevRank(c.sev) > sevRank(a.Sev) {
			a.Acked = false
			raised = append(raised, pend{k, c.dev, c.sev, c.title, name, c.msg})
		}
		a.Sev, a.Title, a.Msg, a.Active = c.sev, c.title, c.msg, true
	}
	var cleared []pend
	for k, a := range h.alerts {
		if _, ok := want[k]; ok { continue }
		if a.Latched {
			a.Active = false
			if a.Acked { delete(h.alerts, k) }
			continue
		}
		cleared = append(cleared, pend{k, a.Device, "info", a.Title, a.DevName, ""})
		delete(h.alerts, k)
	}
	// severidade agregada por equipamento
	for id, s := range h.states {
		sev := "ok"
		if !s.Enabled { sev = "off" } else if !s.Online { sev = "crit"; if now.Sub(s.Since) < time.Duration(r.OfflineGraceSec)*time.Second { sev = "warn" } }
		for _, a := range h.alerts {
			if a.Device == id && sevRank(a.Sev) > sevRank(sevOf(sev)) { sev = a.Sev }
		}
		if s.Severity != sev { s.Severity = sev; h.dirty = true }
	}
	h.dirty = true
	h.mu.Unlock()

	for _, p := range raised {
		h.Log(p.dev, p.name, p.title, p.sev, p.msg, map[string]any{"alert": p.key})
		go h.webhook(p.key, p.sev, p.title, p.name, p.msg)
	}
	for _, p := range cleared {
		h.Log(p.dev, p.name, "NORMALIZADO", "info", p.title+" resolvido.", map[string]any{"alert": p.key})
	}
	for _, p := range latches {
		h.Latch(p.key, p.dev, p.sev, p.title, p.msg)
	}
}

func sevOf(s string) string { if s == "ok" || s == "off" { return "trace" }; return s }

// ---------- difusão ----------

func (h *Hub) Subscribe() chan []byte {
	ch := make(chan []byte, 256)
	h.mu.Lock(); h.subs[ch] = struct{}{}; h.mu.Unlock()
	return ch
}

func (h *Hub) Unsubscribe(ch chan []byte) { h.mu.Lock(); delete(h.subs, ch); h.mu.Unlock() }

// broadcast: chamar com h.mu travado.
func (h *Hub) broadcast(kind string, payload []byte) {
	msg := []byte("event: " + kind + "\ndata: " + string(payload) + "\n\n")
	for ch := range h.subs {
		select { case ch <- msg: default: }
	}
}

type Snapshot struct {
	Update        *UpdInfo        `json:"update"`
	App          string      `json:"app"`
	Version      string      `json:"version"`
	Station      string      `json:"station"`
	Now          time.Time   `json:"now"`
	Started      time.Time   `json:"started"`
	Demo         bool        `json:"demo"`
	Job          *Job        `json:"job"`
	RecArmed     bool        `json:"recArmed"`
	RecArmedAt   time.Time   `json:"recArmedAt"`
	Control      bool        `json:"controlEnabled"`
	CtrlUntil    time.Time   `json:"controlArmedUntil"`
	PinRequired  bool        `json:"pinRequired"`
	Devices      []*DevState `json:"devices"`
	Alerts       []*Alert    `json:"alerts"`
	RecWatch     []string    `json:"recWatch"`
	Pairs        []RecPair   `json:"pairs"`
	Protected    []ProtectedRoute `json:"protected"`
	AuxCfg       []AuxSetting     `json:"auxCfg"`
	MarkerPresets []string        `json:"markerPresets"`
	Rundown      *Rundown         `json:"rundown"`
	Schedule     []ScheduleItem   `json:"schedule"`
	Rules        Rules       `json:"rules"`
	Companion    bool        `json:"companion"`
}

// snapshotLocked: chamar com h.mu travado.
func (h *Hub) snapshotLocked() []byte {
	s := Snapshot{App: "RACK MONITOR BY DON", Version: AppVersion, Station: h.cfg.StationName, Now: time.Now(), Started: h.started, Demo: h.demo,
		Job: h.job, RecArmed: h.recArmed, RecArmedAt: h.recArmedAt, Control: h.cfg.ControlEnabled, CtrlUntil: h.ctrlUntil, PinRequired: h.cfg.ControlPIN != "",
		RecWatch: h.cfg.RecWatch, Pairs: h.cfg.Pairs, Protected: h.cfg.Protected, AuxCfg: h.cfg.AuxCfg, MarkerPresets: h.cfg.MarkerPresets, Rundown: h.rundown, Schedule: h.cfg.Schedule, Rules: h.cfg.Rules, Companion: h.cfg.Companion.Enabled}
	s.Devices, s.Alerts = []*DevState{}, []*Alert{}
	if u := UpdSnapshot(); u.Available || u.Installing { s.Update = &u }
	for _, d := range h.cfg.Devices { if st := h.states[d.ID]; st != nil { s.Devices = append(s.Devices, st) } }
	for _, a := range h.alerts { s.Alerts = append(s.Alerts, a) }
	sort.Slice(s.Alerts, func(i, j int) bool {
		if sevRank(s.Alerts[i].Sev) != sevRank(s.Alerts[j].Sev) { return sevRank(s.Alerts[i].Sev) > sevRank(s.Alerts[j].Sev) }
		return s.Alerts[i].Since.Before(s.Alerts[j].Since)
	})
	b, err := json.Marshal(s)
	if err != nil { log.Printf("snapshot: %v", err); return []byte("{}") }
	return b
}

func (h *Hub) Snapshot() []byte { h.mu.Lock(); defer h.mu.Unlock(); return h.snapshotLocked() }

func (h *Hub) run() {
	tick := time.NewTicker(250 * time.Millisecond)
	eval := time.NewTicker(1 * time.Second)
	evalN := 0
	for {
		select {
		case <-tick.C:
			h.mu.Lock()
			if h.dirty && len(h.subs) > 0 { h.broadcast("state", h.snapshotLocked()) }
			h.dirty = false
			h.mu.Unlock()
		case <-eval.C:
			h.evaluate()
			evalN++
			if evalN%10 == 1 { h.sampleHealth() }
			if evalN%20 == 0 { h.runSchedule() }
		}
	}
}

// ---------- utilidades ----------

func nz(s, def string) string { if strings.TrimSpace(s) == "" { return def }; return s }

func hms(sec int) string {
	if sec < 0 { sec = 0 }
	return fmt.Sprintf("%d:%02d:%02d", sec/3600, (sec/60)%60, sec%60)
}

func human(d time.Duration) string {
	if d < time.Minute { return fmt.Sprintf("%ds", int(d.Seconds())) }
	if d < time.Hour { return fmt.Sprintf("%dmin %ds", int(d.Minutes()), int(d.Seconds())%60) }
	return fmt.Sprintf("%dh %02dmin", int(d.Hours()), int(d.Minutes())%60)
}

func transportPT(t string) string {
	m := map[string]string{"record": "GRAVANDO", "stopped": "PARADO", "preview": "PREVIEW", "play": "PLAY", "forward": "AVANÇO", "rewind": "RETROCESSO", "jog": "JOG", "shuttle": "SHUTTLE"}
	if v, ok := m[t]; ok { return v }
	return strings.ToUpper(nz(t, "?"))
}
