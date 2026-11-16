package main

// Checklist de pré-transmissão, agenda da vigilância, Telegram e perfis de evento.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type ScheduleItem struct {
	Time   string `json:"time"`   // HH:MM
	Action string `json:"action"` // arm | disarm | checklist
	Days   string `json:"days"`   // "" = todos; ou "1234567" (1=segunda … 7=domingo)
	Label  string `json:"label"`
}

type Telegram struct {
	Enabled  bool   `json:"enabled"`
	Token    string `json:"token"`
	ChatID   string `json:"chatId"`
	OnlyCrit bool   `json:"onlyCrit"`
}

// ---------- checklist ----------

type CheckItem struct {
	Group  string `json:"group"`
	Item   string `json:"item"`
	Status string `json:"status"` // ok | warn | crit
	Detail string `json:"detail"`
}

func (h *Hub) RunChecklist(origin string) ([]CheckItem, string) {
	out := []CheckItem{}
	add := func(g, it, st, det string) { out = append(out, CheckItem{g, it, st, det}) }
	h.mu.Lock()
	r := h.cfg.Rules
	for _, d := range h.cfg.Devices {
		if !d.Enabled { continue }
		s := h.states[d.ID]
		if s == nil { continue }
		if !s.Online { add("Rede", d.Name, "crit", "sem conexão — "+nz(s.Detail, "não responde")) } else {
			st := "ok"
			if r.LatencyWarnMs > 0 && s.LatencyMs > float64(r.LatencyWarnMs) { st = "warn" }
			add("Rede", d.Name, st, fmt.Sprintf("online · %.1f ms", s.LatencyMs))
		}
	}
	watch := map[string]bool{}
	for _, id := range h.cfg.RecWatch { watch[id] = true }
	for _, d := range h.cfg.Devices {
		s := h.states[d.ID]
		if s == nil || s.Deck == nil || !s.Online || !d.Enabled { continue }
		dk := s.Deck
		sl := dk.Slots[dk.ActiveSlot]
		g := "Gravação"
		if sl == nil || sl.Status != "mounted" {
			st := "warn"; if watch[d.ID] { st = "crit" }
			add(g, d.Name+" · mídia", st, "slot ativo sem mídia montada")
		} else {
			min := dk.TotalRemain / 60
			st := "ok"
			if min < r.RemainCritMin { st = "crit" } else if min < r.RemainWarnMin { st = "warn" }
			slots := 0
			for _, x := range dk.Slots { if x.Status == "mounted" { slots++ } }
			add(g, d.Name+" · espaço", st, fmt.Sprintf("%s livres em %d slot(s) · %s %s", hms(dk.TotalRemain), slots, dk.VideoFormat, dk.FileFormat))
		}
		if watch[d.ID] {
			st, det := "ok", "em REC"
			if !dk.Recording() { st, det = "warn", "ainda não está gravando ("+transportPT(dk.Transport)+")" }
			add(g, d.Name+" · REC", st, det)
		}
	}
	for _, p := range h.cfg.Pairs {
		a, b := h.states[p.Primary], h.states[p.Backup]
		if a == nil || b == nil || a.Deck == nil || b.Deck == nil { add("Par principal/backup", nz(p.Label, "par"), "crit", "um dos gravadores está fora do ar"); continue }
		if a.Deck.VideoFormat != b.Deck.VideoFormat || a.Deck.FileFormat != b.Deck.FileFormat {
			add("Par principal/backup", nz(p.Label, "par"), "crit", fmt.Sprintf("formatos diferentes: %s %s × %s %s", a.Deck.VideoFormat, a.Deck.FileFormat, b.Deck.VideoFormat, b.Deck.FileFormat))
		} else { add("Par principal/backup", nz(p.Label, "par"), "ok", "mesmo formato e codec: "+a.Deck.VideoFormat+" "+a.Deck.FileFormat) }
	}
	for _, pr := range h.cfg.Protected {
		s := h.states[pr.Device]
		if s == nil || s.Hub == nil { continue }
		lbl := fmt.Sprintf("%s · saída %d %s", s.Name, pr.Output+1, s.Hub.OutLabels[pr.Output])
		cur := s.Hub.Routes[pr.Output]
		if pr.Expected < 0 { add("Rotas", lbl, "ok", "vigiada · agora "+s.Hub.InLabels[cur]); continue }
		if cur != pr.Expected { add("Rotas", lbl, "crit", fmt.Sprintf("recebe %d %s; esperado %d %s", cur+1, s.Hub.InLabels[cur], pr.Expected+1, s.Hub.InLabels[pr.Expected])) } else {
			add("Rotas", lbl, "ok", "travada em "+s.Hub.InLabels[cur])
		}
	}
	for _, ax := range h.cfg.AuxCfg {
		s := h.states[ax.Device]
		if s == nil || s.Atem == nil || ax.Mode != "lock" { continue }
		cur := s.Atem.Aux[ax.Aux]
		lbl := s.Name + " · " + auxTitle(s.Atem, ax.Aux, ax)
		if cur != ax.Expected { add("AUX", lbl, "crit", "em "+s.Atem.Name(cur)+"; esperado "+s.Atem.Name(ax.Expected)) } else { add("AUX", lbl, "ok", "travado em "+s.Atem.Name(cur)) }
	}
	for _, d := range h.cfg.Devices {
		s := h.states[d.ID]
		if s == nil || s.Atem == nil || !s.Online { continue }
		for me, m := range s.Atem.MEs { if m.FTB { add("ATEM", fmt.Sprintf("%s · M/E %d", d.Name, me+1), "warn", "FADE TO BLACK ativo") } }
		if s.Atem.Stream != "" { st := "ok"; if s.Atem.Stream != "STREAMING" { st = "warn" }; add("ATEM", d.Name+" · stream", st, s.Atem.Stream) }
	}
	h.mu.Unlock()
	nc, nw := 0, 0
	for _, x := range out { if x.Status == "crit" { nc++ } else if x.Status == "warn" { nw++ } }
	verdict, sev := "PRONTO PARA O AR", "info"
	if nc > 0 { verdict, sev = fmt.Sprintf("NÃO PRONTO — %d item(ns) crítico(s)", nc), "crit" } else if nw > 0 { verdict, sev = fmt.Sprintf("PRONTO COM %d AVISO(S)", nw), "warn" }
	raw, _ := json.Marshal(out)
	var items []any
	json.Unmarshal(raw, &items)
	h.LogO(origin, "", "RACK MONITOR", "CHECKLIST", sev, fmt.Sprintf("%s · %d itens conferidos", verdict, len(out)), map[string]any{"items": items, "verdict": verdict})
	return out, verdict
}

// ---------- agenda ----------

var schedDone = map[string]string{}

func (h *Hub) runSchedule() {
	now := time.Now()
	hm := now.Format("15:04")
	wd := int(now.Weekday())
	if wd == 0 { wd = 7 }
	cfgMu.RLock()
	items := append([]ScheduleItem(nil), h.cfg.Schedule...)
	nWatch := len(h.cfg.RecWatch)
	cfgMu.RUnlock()
	for i, it := range items {
		if it.Time != hm { continue }
		if it.Days != "" && !strings.Contains(it.Days, fmt.Sprint(wd)) { continue }
		key := fmt.Sprintf("%d|%s|%s", i, it.Time, it.Action)
		day := now.Format("2006-01-02")
		if schedDone[key] == day { continue }
		schedDone[key] = day
		lbl := nz(it.Label, "agenda")
		switch it.Action {
		case "arm":
			if nWatch == 0 { h.LogO("agenda", "", "RACK MONITOR", "AGENDA", "warn", "Não armou: nenhum gravador definido para a vigilância.", nil); continue }
			h.mu.Lock(); h.recArmed = true; h.recArmedAt = now; h.dirty = true; h.mu.Unlock()
			h.LogO("agenda", "", "RACK MONITOR", "VIGILÂNCIA ARMADA", "warn", fmt.Sprintf("Armada automaticamente às %s (%s).", hm, lbl), nil)
		case "disarm":
			h.mu.Lock(); h.recArmed = false; h.dirty = true; h.mu.Unlock()
			h.LogO("agenda", "", "RACK MONITOR", "VIGILÂNCIA DESARMADA", "info", fmt.Sprintf("Desarmada automaticamente às %s (%s).", hm, lbl), nil)
		case "checklist":
			h.RunChecklist("agenda")
		}
	}
}

// ---------- Telegram ----------

func sendTelegram(t Telegram, text string) error {
	if t.Token == "" || t.ChatID == "" { return fmt.Errorf("informe o token do bot e o chat id") }
	u := "https://api.telegram.org/bot" + url.PathEscape(t.Token) + "/sendMessage"
	b, _ := json.Marshal(map[string]any{"chat_id": t.ChatID, "text": text, "disable_web_page_preview": true})
	cl := http.Client{Timeout: 6 * time.Second}
	resp, err := cl.Post(u, "application/json", bytes.NewReader(b))
	if err != nil { return err }
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		var e struct{ Description string `json:"description"` }
		json.NewDecoder(resp.Body).Decode(&e)
		return fmt.Errorf("Telegram recusou (%d): %s", resp.StatusCode, e.Description)
	}
	return nil
}

func (h *Hub) telegramAlert(sev, title, dev, msg string) {
	cfgMu.RLock()
	t := h.cfg.Telegram
	st := h.cfg.StationName
	cfgMu.RUnlock()
	if !t.Enabled || sev == "info" || (t.OnlyCrit && sev != "crit") { return }
	icon := "⚠️"
	if sev == "crit" { icon = "🔴" }
	_ = sendTelegram(t, fmt.Sprintf("%s %s\n%s\n%s\n— %s · %s", icon, title, nz(dev, "RACK"), msg, st, time.Now().Format("15:04:05")))
}

// ---------- perfis de evento ----------

type Profile struct {
	Name          string           `json:"name"`
	Saved         time.Time        `json:"saved"`
	Devices       []Device         `json:"devices"`
	Rules         Rules            `json:"rules"`
	RecWatch      []string         `json:"recWatch"`
	Pairs         []RecPair        `json:"pairs"`
	Protected     []ProtectedRoute `json:"protected"`
	AuxCfg        []AuxSetting     `json:"auxCfg"`
	Schedule      []ScheduleItem   `json:"schedule"`
	MarkerPresets []string         `json:"markerPresets"`
}

func (h *Hub) profDir() string { d := filepath.Join(h.paths.Root, "perfis"); _ = os.MkdirAll(d, 0o755); return d }

func (h *Hub) ListProfiles() []Profile {
	ents, _ := os.ReadDir(h.profDir())
	out := []Profile{}
	for _, e := range ents {
		if !strings.HasSuffix(e.Name(), ".json") { continue }
		b, err := os.ReadFile(filepath.Join(h.profDir(), e.Name()))
		if err != nil { continue }
		var p Profile
		if json.Unmarshal(b, &p) == nil { p.Devices = append([]Device(nil), p.Devices...); out = append(out, p) }
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Name < out[b].Name })
	return out
}

func (h *Hub) SaveProfile(name string) (*Profile, error) {
	name = strings.TrimSpace(name)
	if name == "" { return nil, fmt.Errorf("dê um nome ao perfil") }
	cfgMu.RLock()
	c := h.cfg
	p := &Profile{Name: name, Saved: time.Now(), Devices: c.Devices, Rules: c.Rules, RecWatch: c.RecWatch, Pairs: c.Pairs, Protected: c.Protected, AuxCfg: c.AuxCfg, Schedule: c.Schedule, MarkerPresets: c.MarkerPresets}
	b, _ := json.MarshalIndent(p, "", "  ")
	cfgMu.RUnlock()
	return p, os.WriteFile(filepath.Join(h.profDir(), slug(name)+".json"), b, 0o644)
}

func (h *Hub) LoadProfile(p Profile) {
	cfgMu.Lock()
	c := h.cfg
	c.Devices, c.Rules, c.RecWatch, c.Pairs, c.Protected, c.AuxCfg, c.Schedule = p.Devices, p.Rules, p.RecWatch, p.Pairs, p.Protected, p.AuxCfg, p.Schedule
	if len(p.MarkerPresets) > 0 { c.MarkerPresets = p.MarkerPresets }
	normalize(c)
	_ = saveConfig(h.paths, c)
	cfgMu.Unlock()
	h.applyDevices()
}

func (h *Hub) ReadProfile(name string) (*Profile, error) {
	b, err := os.ReadFile(filepath.Join(h.profDir(), slug(name)+".json"))
	if err != nil { return nil, fmt.Errorf("perfil não encontrado") }
	var p Profile
	if err := json.Unmarshal(b, &p); err != nil { return nil, err }
	return &p, nil
}

func (h *Hub) DeleteProfile(name string) error { return os.Remove(filepath.Join(h.profDir(), slug(name)+".json")) }
