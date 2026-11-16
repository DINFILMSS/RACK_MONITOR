package main

// SNMP v2c mínimo (GET) — sem bibliotecas externas.
// • NOBREAK: UPS-MIB padrão (RFC 1628) com alternativa APC PowerNet.
// • SWITCH: IF-MIB (porta ligada/desligada, velocidade, erros) das portas
//   escolhidas — ex.: a porta que liga a ATEM, o HyperDeck, o painel.

import (
	"errors"
	"fmt"
	"math/rand"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ---------- BER ----------

func berLen(n int) []byte {
	if n < 128 { return []byte{byte(n)} }
	var b []byte
	for n > 0 { b = append([]byte{byte(n)}, b...); n >>= 8 }
	return append([]byte{0x80 | byte(len(b))}, b...)
}
func tlv(tag byte, v []byte) []byte { return append(append([]byte{tag}, berLen(len(v))...), v...) }
func berInt(n int64) []byte {
	b := []byte{byte(n)}
	for n > 127 || n < -128 { n >>= 8; b = append([]byte{byte(n)}, b...) }
	return tlv(0x02, b)
}
func berOID(oid string) ([]byte, error) {
	p := strings.Split(strings.Trim(oid, "."), ".")
	if len(p) < 2 { return nil, errors.New("oid inválido") }
	nums := make([]int, len(p))
	for i, s := range p { n, err := strconv.Atoi(s); if err != nil { return nil, err }; nums[i] = n }
	out := []byte{byte(nums[0]*40 + nums[1])}
	for _, n := range nums[2:] {
		var t []byte
		t = append(t, byte(n&0x7f))
		for n >>= 7; n > 0; n >>= 7 { t = append([]byte{byte(n&0x7f | 0x80)}, t...) }
		out = append(out, t...)
	}
	return tlv(0x06, out), nil
}
func decOID(b []byte) string {
	if len(b) == 0 { return "" }
	s := []string{strconv.Itoa(int(b[0]) / 40), strconv.Itoa(int(b[0]) % 40)}
	n := 0
	for _, x := range b[1:] { n = n<<7 | int(x&0x7f); if x&0x80 == 0 { s = append(s, strconv.Itoa(n)); n = 0 } }
	return strings.Join(s, ".")
}

type berTLV struct{ tag byte; val []byte }

func berRead(b []byte) (berTLV, []byte, error) {
	if len(b) < 2 { return berTLV{}, nil, errors.New("curto") }
	tag, l, off := b[0], int(b[1]), 2
	if l&0x80 != 0 {
		n := l & 0x7f
		if n > 4 || len(b) < 2+n { return berTLV{}, nil, errors.New("tamanho") }
		l = 0
		for i := 0; i < n; i++ { l = l<<8 | int(b[2+i]) }
		off = 2 + n
	}
	if len(b) < off+l { return berTLV{}, nil, errors.New("truncado") }
	return berTLV{tag, b[off : off+l]}, b[off+l:], nil
}
func berIntVal(b []byte) int64 {
	var n int64
	if len(b) > 0 && b[0]&0x80 != 0 { n = -1 }
	for _, x := range b { n = n<<8 | int64(x) }
	return n
}
func berUint(b []byte) int64 { var n int64; for _, x := range b { n = n<<8 | int64(x) }; return n }

// snmpMsg monta uma mensagem v2c (pdu 0xA0 = GET, 0xA2 = resposta).
func snmpMsg(community string, pdu byte, reqID int64, vbs [][2][]byte) []byte {
	var list []byte
	for _, vb := range vbs { list = append(list, tlv(0x30, append(append([]byte{}, vb[0]...), vb[1]...))...) }
	p := append(append(append(berInt(reqID), berInt(0)...), berInt(0)...), tlv(0x30, list)...)
	return tlv(0x30, append(append(berInt(1), tlv(0x04, []byte(community))...), tlv(pdu, p)...))
}

// snmpParse devolve community, pdu, reqID e varbinds (oid → valor).
func snmpParse(b []byte) (string, byte, int64, []string, map[string]any, error) {
	msg, _, err := berRead(b)
	if err != nil || msg.tag != 0x30 { return "", 0, 0, nil, nil, errors.New("mensagem inválida") }
	_, rest, _ := berRead(msg.val) // versão
	cm, rest, err := berRead(rest)
	if err != nil { return "", 0, 0, nil, nil, err }
	pdu, _, err := berRead(rest)
	if err != nil { return "", 0, 0, nil, nil, err }
	rid, r2, _ := berRead(pdu.val)
	_, r2, _ = berRead(r2)
	_, r2, _ = berRead(r2)
	vbl, _, err := berRead(r2)
	if err != nil { return "", 0, 0, nil, nil, err }
	vals := map[string]any{}
	var order []string
	for rest := vbl.val; len(rest) > 0; {
		var vb berTLV
		vb, rest, err = berRead(rest)
		if err != nil { break }
		o, r3, _ := berRead(vb.val)
		v, _, _ := berRead(r3)
		oid := decOID(o.val)
		order = append(order, oid)
		switch v.tag {
		case 0x02: vals[oid] = berIntVal(v.val)
		case 0x04: vals[oid] = string(v.val)
		case 0x41, 0x42, 0x43, 0x46: vals[oid] = berUint(v.val)
		case 0x06: vals[oid] = decOID(v.val)
		default: // NULL, noSuchObject, noSuchInstance
		}
	}
	return string(cm.val), pdu.tag, berIntVal(rid.val), order, vals, nil
}

func snmpGet(host string, port int, community string, oids []string) (map[string]any, float64, error) {
	if port == 0 { port = 161 }
	if community == "" { community = "public" }
	out := map[string]any{}
	var lat float64
	for i := 0; i < len(oids); i += 20 { // até 20 OIDs por pacote
		chunk := oids[i:min(i+20, len(oids))]
		var vbs [][2][]byte
		for _, o := range chunk { e, err := berOID(o); if err != nil { return nil, 0, err }; vbs = append(vbs, [2][]byte{e, {0x05, 0x00}}) }
		id := rand.Int63n(1 << 30)
		c, err := net.Dial("udp", net.JoinHostPort(host, strconv.Itoa(port)))
		if err != nil { return nil, 0, err }
		t0 := time.Now()
		c.Write(snmpMsg(community, 0xA0, id, vbs))
		c.SetReadDeadline(time.Now().Add(1500 * time.Millisecond))
		buf := make([]byte, 65535)
		n, err := c.Read(buf)
		c.Close()
		if err != nil { return nil, 0, fmt.Errorf("sem resposta SNMP (community ou IP errado?)") }
		lat = float64(time.Since(t0).Microseconds()) / 1000
		_, _, rid, _, vals, err := snmpParse(buf[:n])
		if err != nil || rid != id { return nil, 0, fmt.Errorf("resposta SNMP inválida") }
		for k, v := range vals { out[k] = v }
	}
	return out, lat, nil
}

// ---------- nobreak / switch ----------

type UPSState struct {
	Model        string `json:"model"`
	Source       string `json:"source"`
	OnBattery    bool   `json:"onBattery"`
	BatteryLow   bool   `json:"batteryLow"`
	Charge       int    `json:"charge"`
	MinutesLeft  int    `json:"minutesLeft"`
	SecOnBattery int    `json:"secOnBattery"`
	Load         int    `json:"load"`
}

type PortState struct {
	Index    int    `json:"index"`
	Name     string `json:"name"`
	Alias    string `json:"alias"`
	Up       bool   `json:"up"`
	Speed    int    `json:"speed"`
	InErrors int64  `json:"inErrors"`
	ErrDelta int64  `json:"errDelta"`
	Known    bool   `json:"known"`
}

type SnmpState struct {
	SysName  string      `json:"sysName"`
	SysDescr string      `json:"sysDescr"`
	UPS      *UPSState   `json:"ups,omitempty"`
	Ports    []PortState `json:"ports,omitempty"`
}

func parsePorts(s string) []int {
	var out []int
	for _, part := range strings.Split(strings.ReplaceAll(s, " ", ""), ",") {
		if a, b, ok := strings.Cut(part, "-"); ok {
			x, _ := strconv.Atoi(a); y, _ := strconv.Atoi(b)
			for i := x; i <= y && i-x < 128; i++ { if i > 0 { out = append(out, i) } }
		} else if n, err := strconv.Atoi(part); err == nil && n > 0 { out = append(out, n) }
	}
	sort.Ints(out)
	return out
}

func gi(m map[string]any, k string) (int64, bool) { v, ok := m[k].(int64); return v, ok }
func gs(m map[string]any, k string) string { v, _ := m[k].(string); return strings.TrimSpace(v) }

func startSNMP(h *Hub, d Device) func() {
	stop := make(chan struct{})
	go func() {
		fails := 0
		prevErr := map[int]int64{}
		prevUp := map[int]bool{}
		for {
			oids := []string{"1.3.6.1.2.1.1.1.0", "1.3.6.1.2.1.1.5.0"}
			ups := "1.3.6.1.2.1.33.1."
			apc := "1.3.6.1.4.1.318.1.1.1."
			ports := parsePorts(d.Ports)
			if d.Kind == "ups" {
				oids = append(oids, ups+"1.1.0", ups+"1.2.0", ups+"4.1.0", ups+"2.1.0", ups+"2.2.0", ups+"2.3.0", ups+"2.4.0", ups+"4.4.1.5.1",
					apc+"1.1.1.0", apc+"4.1.1.0", apc+"2.2.1.0", apc+"2.2.3.0", apc+"4.2.3.0")
			} else {
				for _, p := range ports {
					ps := strconv.Itoa(p)
					oids = append(oids, "1.3.6.1.2.1.2.2.1.2."+ps, "1.3.6.1.2.1.2.2.1.8."+ps, "1.3.6.1.2.1.2.2.1.14."+ps, "1.3.6.1.2.1.31.1.1.1.18."+ps, "1.3.6.1.2.1.31.1.1.1.15."+ps)
				}
			}
			v, lat, err := snmpGet(d.Host, d.Port, d.Community, oids)
			if err != nil {
				fails++
				if fails >= 2 { h.SetOnline(d.ID, false, err.Error()) }
			} else {
				fails = 0
				type lg struct{ typ, sev, msg string }
				var logs []lg
				h.Update(d.ID, func(s *DevState) {
					s.LatencyMs = lat
					sn := &SnmpState{SysDescr: gs(v, "1.3.6.1.2.1.1.1.0"), SysName: gs(v, "1.3.6.1.2.1.1.5.0")}
					if d.Kind == "ups" {
						u := &UPSState{Model: strings.TrimSpace(gs(v, ups+"1.1.0") + " " + gs(v, ups+"1.2.0")), Charge: -1, MinutesLeft: -1}
						if src, ok := gi(v, ups+"4.1.0"); ok {
							u.Source = map[int64]string{1: "OUTRA", 2: "SEM SAÍDA", 3: "REDE ELÉTRICA", 4: "BYPASS", 5: "BATERIA", 6: "BOOST", 7: "REDUTOR"}[src]
							u.OnBattery = src == 5
							if b, ok := gi(v, ups+"2.1.0"); ok { u.BatteryLow = b == 3 || b == 4 }
							if x, ok := gi(v, ups+"2.2.0"); ok { u.SecOnBattery = int(x) }
							if x, ok := gi(v, ups+"2.3.0"); ok { u.MinutesLeft = int(x) }
							if x, ok := gi(v, ups+"2.4.0"); ok { u.Charge = int(x) }
							if x, ok := gi(v, ups+"4.4.1.5.1"); ok { u.Load = int(x) }
						} else if st, ok := gi(v, apc+"4.1.1.0"); ok { // APC PowerNet
							u.Model = nz(gs(v, apc+"1.1.1.0"), "APC")
							u.Source = map[int64]string{2: "REDE ELÉTRICA", 3: "BATERIA", 4: "BOOST", 6: "BYPASS", 12: "REDUTOR"}[st]
							u.OnBattery = st == 3
							if x, ok := gi(v, apc+"2.2.1.0"); ok { u.Charge = int(x) }
							if x, ok := gi(v, apc+"2.2.3.0"); ok { u.MinutesLeft = int(x / 6000) }
							if x, ok := gi(v, apc+"4.2.3.0"); ok { u.Load = int(x) }
						} else { u.Source = "?" }
						if s.Snmp != nil && s.Snmp.UPS != nil && s.Snmp.UPS.OnBattery != u.OnBattery {
							if u.OnBattery { logs = append(logs, lg{"NOBREAK", "crit", fmt.Sprintf("Faltou energia: nobreak na BATERIA (carga %d%%, autonomia %d min)", u.Charge, u.MinutesLeft)}) } else {
								logs = append(logs, lg{"NOBREAK", "info", "Energia voltou: nobreak na rede elétrica"})
							}
						}
						sn.UPS = u
						s.Detail = nz(u.Model, sn.SysName)
					} else {
						for _, p := range ports {
							ps := strconv.Itoa(p)
							st, ok := gi(v, "1.3.6.1.2.1.2.2.1.8."+ps)
							x := PortState{Index: p, Known: ok, Up: st == 1, Name: gs(v, "1.3.6.1.2.1.2.2.1.2."+ps), Alias: gs(v, "1.3.6.1.2.1.31.1.1.1.18."+ps)}
							if sp, ok := gi(v, "1.3.6.1.2.1.31.1.1.1.15."+ps); ok { x.Speed = int(sp) }
							if e, ok := gi(v, "1.3.6.1.2.1.2.2.1.14."+ps); ok {
								x.InErrors = e
								if pe, ok := prevErr[p]; ok && e > pe { x.ErrDelta = e - pe }
								prevErr[p] = e
							}
							nm := nz(x.Alias, nz(x.Name, "porta "+ps))
							if was, ok := prevUp[p]; ok && x.Known && was != x.Up {
								if x.Up { logs = append(logs, lg{"PORTA DO SWITCH", "info", fmt.Sprintf("Porta %d (%s) voltou (%d Mb/s)", p, nm, x.Speed)}) } else {
									logs = append(logs, lg{"PORTA DO SWITCH", "crit", fmt.Sprintf("Porta %d (%s) CAIU — link desligado", p, nm)})
								}
							}
							if x.Known { prevUp[p] = x.Up }
							if x.ErrDelta > 0 { logs = append(logs, lg{"ERROS NA PORTA", "warn", fmt.Sprintf("Porta %d (%s): +%d erros de recepção (cabo/conector?)", p, nm, x.ErrDelta)}) }
							sn.Ports = append(sn.Ports, x)
						}
						s.Detail = nz(sn.SysName, strings.SplitN(sn.SysDescr, "\n", 2)[0])
					}
					s.Snmp = sn
				})
				h.SetOnline(d.ID, true, "")
				for _, l := range logs { h.LogO("externo", d.ID, d.Name, l.typ, l.sev, l.msg, nil) }
			}
			select { case <-stop: return; case <-time.After(5 * time.Second): }
		}
	}()
	return func() { close(stop) }
}
