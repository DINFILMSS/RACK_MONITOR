package main

// RACK MONITOR BY DON V2 — configuração persistente.
// Tudo fica em %LOCALAPPDATA%\DON_RACK_MONITOR\v2 (o legado V0.x não é tocado).

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

type Device struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"` // atem | hyperdeck | videohub | tcp | http
	Host    string `json:"host"`
	Port    int    `json:"port"`
	Group   string `json:"group"`
	Enabled bool   `json:"enabled"`
	Notes   string `json:"notes"`
	Community string `json:"community,omitempty"` // SNMP (nobreak/switch)
	Ports     string `json:"ports,omitempty"`     // switch: portas a vigiar (ex.: "1,2,24")
	Via       string `json:"via,omitempty"`       // "" = conexão direta · "companion" = lê do Companion
	CompLabel string `json:"compLabel,omitempty"` // nome da conexão no Companion (ex.: hyperdeck_5_PRODUZ)
}

type RecPair struct {
	Primary string `json:"primary"`
	Backup  string `json:"backup"`
	Label   string `json:"label"`
}

type ProtectedRoute struct {
	Device   string `json:"device"`
	Output   int    `json:"output"`   // base 0 (protocolo)
	Expected int    `json:"expected"` // base 0; -1 = apenas alertar mudanças
}

// AuxSetting: como o monitor trata cada AUX de cada ATEM.
// Mode: "monitor" (padrão) | "ignore" (some da tela, do log, do relatório e do Companion)
//       | "lock" (alerta crítico se sair da fonte esperada)
type AuxSetting struct {
	Device   string `json:"device"`
	Aux      int    `json:"aux"` // base 0
	Label    string `json:"label"`
	Mode     string `json:"mode"`
	Expected int    `json:"expected"`
}

type Rules struct {
	OfflineGraceSec   int  `json:"offlineGraceSec"`
	RemainWarnMin     int  `json:"remainWarnMin"`
	RemainCritMin     int  `json:"remainCritMin"`
	SlotWarnMin       int  `json:"slotWarnMin"`
	TCStallSec        int  `json:"tcStallSec"`
	PairMismatchSec   int  `json:"pairMismatchSec"`
	LatencyWarnMs     int  `json:"latencyWarnMs"`
	AlertFTB          bool `json:"alertFTB"`
	LogPreview        bool `json:"logPreview"`
	CCUOff            bool `json:"ccuOff"`
	CCUMinPct         int  `json:"ccuMinPct"`
	ForgotRecOff      bool `json:"forgotRecOff"`    // desliga o alarme "esqueceu de gravar"
	ForgotRecSec      int  `json:"forgotRecSec"`    // tolerância após o início
	ForgotRecNoATEM   bool `json:"forgotRecNoAtem"` // não usar "ATEM saiu do preto" como início
	SoundOnCritical   bool `json:"soundOnCritical"`
	StreamDropIsCrit  bool `json:"streamDropIsCrit"`
	FormatMismatchCrit bool `json:"formatMismatchCrit"`
}

type Companion struct {
	Enabled bool   `json:"enabled"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
	Prefix  string `json:"prefix"`
}

type Config struct {
	Version           int              `json:"version"`
	HTTPPort          int              `json:"httpPort"`
	Bind              string           `json:"bind"`
	StationName       string           `json:"stationName"`
	Devices           []Device         `json:"devices"`
	Rules             Rules            `json:"rules"`
	RecWatch          []string         `json:"recWatch"` // decks que DEVEM estar gravando quando a vigilância está armada
	Pairs             []RecPair        `json:"pairs"`
	Protected         []ProtectedRoute `json:"protected"`
	AuxCfg            []AuxSetting     `json:"auxCfg"`
	Companion         Companion        `json:"companion"`
	WebhookURL        string           `json:"webhookUrl"`
	Telegram          Telegram         `json:"telegram"`
	Schedule          []ScheduleItem   `json:"schedule"`
	MarkerPresets     []string         `json:"markerPresets"`
	TSL               []TSLTarget      `json:"tsl"`
	Email             EmailCfg         `json:"email"`
	ReportOnClose     bool             `json:"reportOnClose"`
	ClipTemplate      string           `json:"clipTemplate"` // nome automático dos clipes
	ClipNameOff       bool             `json:"clipNameOff"`
	UpdateRepo        string           `json:"updateRepo"`     // usuario/repositorio no GitHub
	UpdateCheckOff    bool             `json:"updateCheckOff"` // não procurar versões novas
	UpdateAuto        bool             `json:"updateAuto"`     // instalar sozinho (só sem REC)
	ControlEnabled    bool             `json:"controlEnabled"`
	AllowRemoteControl bool            `json:"allowRemoteControl"`
	ControlPIN        string           `json:"controlPin"`
	CurrentJob        string           `json:"currentJob"`
}

var cfgMu sync.RWMutex

func defaultRules() Rules {
	return Rules{OfflineGraceSec: 5, RemainWarnMin: 30, RemainCritMin: 10, SlotWarnMin: 5, TCStallSec: 4, PairMismatchSec: 3,
		LatencyWarnMs: 250, AlertFTB: true, LogPreview: false, SoundOnCritical: true, StreamDropIsCrit: true, FormatMismatchCrit: false}
}

func defaultConfig() *Config {
	c := &Config{Version: 2, HTTPPort: 8787, Bind: "0.0.0.0", StationName: hostName(), Rules: defaultRules(),
		Companion: Companion{Host: "127.0.0.1", Port: 8000, Prefix: "rack_"}}
	add := func(name, kind, ip string, port int) {
		c.Devices = append(c.Devices, Device{ID: uniqueID(c.Devices, name), Name: name, Kind: kind, Host: ip, Port: port, Enabled: true})
	}
	// Mesmo inventário de fábrica da V0.7.8.
	add("ATEM 1 - Constellation 8K", "atem", "192.168.10.59", 9910)
	add("ATEM 2 - Constellation", "atem", "192.168.10.58", 9910)
	add("VideoHub - Matriz 1 40x40", "videohub", "192.168.10.215", 9990)
	add("Smart VideoHub CleanSwitch 12x12", "videohub", "192.168.10.146", 9990)
	add("HyperDeck - PGM SUJO", "hyperdeck", "192.168.10.52", 9993)
	add("HyperDeck - SUJO BKP", "hyperdeck", "192.168.10.53", 9993)
	add("HyperDeck - PRODUZIDO LIMPO", "hyperdeck", "192.168.10.54", 9993)
	add("HyperDeck - PGM LIMPO", "hyperdeck", "192.168.10.55", 9993)
	add("HyperDeck - GRUA", "hyperdeck", "192.168.10.56", 9993)
	return c
}

func hostName() string { h, _ := os.Hostname(); if h == "" { return "ESTAÇÃO" }; return h }

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func slug(s string) string {
	s = strings.ToLower(s)
	r := strings.NewReplacer("á", "a", "à", "a", "ã", "a", "â", "a", "é", "e", "ê", "e", "í", "i", "ó", "o", "ô", "o", "õ", "o", "ú", "u", "ç", "c")
	s = slugRe.ReplaceAllString(r.Replace(s), "_")
	s = strings.Trim(s, "_")
	if len(s) > 32 { s = strings.Trim(s[:32], "_") }
	if s == "" { s = "dev" }
	return s
}

func uniqueID(list []Device, name string) string {
	base := slug(name); id := base
	for n := 2; ; n++ {
		taken := false
		for _, d := range list { if d.ID == id { taken = true; break } }
		if !taken { return id }
		id = base + "_" + strconv.Itoa(n)
	}
}

func defaultPort(kind string) int {
	switch kind {
	case "atem": return 9910
	case "hyperdeck": return 9993
	case "videohub": return 9990
	case "http": return 80
	case "bmcamera": return 80
	case "webpresenter": return 9977
	case "ups", "switch": return 161
	}
	return 0
}

type Paths struct{ Root, Config, Jobs, Legacy string }

func resolvePaths(demo bool, override string) Paths {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" { if d, err := os.UserConfigDir(); err == nil { base = d } else { base = "." } }
	legacy := filepath.Join(base, "DON_RACK_MONITOR")
	root := filepath.Join(legacy, "v2")
	if demo { root = filepath.Join(legacy, "v2_demo") }
	if override != "" { root = override }
	return Paths{Root: root, Config: filepath.Join(root, "config.json"), Jobs: filepath.Join(root, "jobs"), Legacy: legacy}
}

func loadConfig(p Paths) (*Config, []string) {
	notes := []string{}
	_ = os.MkdirAll(p.Jobs, 0o755)
	b, err := os.ReadFile(p.Config)
	if err == nil {
		c := defaultConfig(); c.Devices = nil
		if json.Unmarshal(b, c) == nil {
			normalize(c)
			return c, notes
		}
		_ = os.Rename(p.Config, p.Config+".corrompido")
		notes = append(notes, "config.json ilegível foi preservado como config.json.corrompido")
	}
	c := defaultConfig()
	if devs := importLegacyDevices(filepath.Join(p.Legacy, "equipamentos.tsv")); len(devs) > 0 {
		c.Devices = devs
		notes = append(notes, fmt.Sprintf("%d equipamentos importados da V0.7.x (equipamentos.tsv)", len(devs)))
		if pr := importLegacyPair(filepath.Join(p.Legacy, "par_gravacao.tsv"), devs); pr != nil {
			c.Pairs = append(c.Pairs, *pr); c.RecWatch = []string{pr.Primary, pr.Backup}
			notes = append(notes, "par de gravação principal/backup importado da V0.7.x")
		}
	}
	normalize(c)
	_ = saveConfig(p, c)
	return c, notes
}

func normalize(c *Config) {
	if c.HTTPPort <= 0 { c.HTTPPort = 8787 }
	if c.Bind == "" { c.Bind = "0.0.0.0" }
	if c.Rules.OfflineGraceSec <= 0 { c.Rules = defaultRules() }
	if c.Rules.SlotWarnMin == 0 { c.Rules.SlotWarnMin = 5 }
	if c.Rules.CCUMinPct <= 0 { c.Rules.CCUMinPct = 5 }
	if c.Rules.ForgotRecSec <= 0 { c.Rules.ForgotRecSec = 10 }
	if strings.TrimSpace(c.ClipTemplate) == "" { c.ClipTemplate = "{JOB}_{DECK}_{NNN}" }
	if c.Companion.Port == 0 { c.Companion.Port = 8000 }
	if c.Companion.Prefix == "" { c.Companion.Prefix = "rack_" }
	if len(c.MarkerPresets) == 0 { c.MarkerPresets = []string{"GOL", "INTERVALO", "DESTAQUE", "REPLAY", "FALHA DE ÁUDIO", "FALHA DE VÍDEO"} }
	sc := []ScheduleItem{}
	for _, s := range c.Schedule { if len(s.Time) == 5 && s.Time[2] == ':' { sc = append(sc, s) } }
	c.Schedule = sc
	seen := map[string]bool{}
	out := []Device{}
	for _, d := range c.Devices {
		d.Name = strings.TrimSpace(d.Name)
		if d.Name == "" { continue }
		d.Kind = strings.ToLower(strings.TrimSpace(d.Kind))
		if d.Port <= 0 || d.Port > 65535 { d.Port = defaultPort(d.Kind) }
		if d.ID == "" || seen[d.ID] { d.ID = uniqueID(out, d.Name) }
		seen[d.ID] = true
		out = append(out, d)
	}
	c.Devices = out
	// listas nunca nulas (a interface depende disso)
	if c.RecWatch == nil { c.RecWatch = []string{} }
	if c.Pairs == nil { c.Pairs = []RecPair{} }
	if c.Protected == nil { c.Protected = []ProtectedRoute{} }
	if c.TSL == nil { c.TSL = []TSLTarget{} }
	ax := []AuxSetting{}
	for _, a := range c.AuxCfg {
		a.Label = strings.TrimSpace(a.Label)
		if a.Mode != "ignore" && a.Mode != "lock" { a.Mode = "monitor" }
		if a.Mode == "monitor" && a.Label == "" { continue }
		ax = append(ax, a)
	}
	c.AuxCfg = ax
}

// auxMap: chamar com cfgMu (leitura) ou sem concorrência de escrita.
func (c *Config) auxMap(dev string) map[int]AuxSetting {
	m := map[int]AuxSetting{}
	for _, a := range c.AuxCfg { if a.Device == dev { m[a.Aux] = a } }
	return m
}

func auxLabel(i int, s AuxSetting) string {
	if s.Label != "" { return fmt.Sprintf("AUX %d (%s)", i+1, s.Label) }
	return fmt.Sprintf("AUX %d", i+1)
}

func saveConfig(p Paths, c *Config) error {
	_ = os.MkdirAll(p.Root, 0o755)
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil { return err }
	tmp := p.Config + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil { return err }
	return os.Rename(tmp, p.Config)
}

func importLegacyDevices(path string) []Device {
	f, err := os.Open(path)
	if err != nil { return nil }
	defer f.Close()
	var out []Device
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		parts := strings.Split(strings.TrimPrefix(sc.Text(), "\ufeff"), "\t")
		if len(parts) < 4 { continue }
		port, err := strconv.Atoi(strings.TrimSpace(parts[3]))
		if err != nil || port <= 0 { continue }
		out = append(out, Device{ID: uniqueID(out, parts[0]), Name: parts[0], Kind: strings.ToLower(parts[1]), Host: parts[2], Port: port, Enabled: true})
	}
	return out
}

func importLegacyPair(path string, devs []Device) *RecPair {
	b, err := os.ReadFile(path)
	if err != nil { return nil }
	lines := strings.Split(strings.ReplaceAll(string(b), "\r", ""), "\n")
	if len(lines) < 2 { return nil }
	find := func(ip string) string {
		ip = strings.TrimSpace(ip)
		for _, d := range devs { if d.Kind == "hyperdeck" && (d.Host == ip || d.Name == ip) { return d.ID } }
		return ""
	}
	a, b2 := find(lines[0]), find(lines[1])
	if a == "" || b2 == "" || a == b2 { return nil }
	return &RecPair{Primary: a, Backup: b2, Label: "PGM principal / backup"}
}
