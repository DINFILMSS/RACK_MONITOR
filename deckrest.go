package main

// Formatação pela REST API do HyperDeck (firmware 8.4+): http://IP/control/api/v1
// É um canal separado da porta 9993 — funciona mesmo com o Companion ocupando
// a conexão única do protocolo HyperDeck (modo "Via Companion").

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type DeckMedia struct {
	Index      int    `json:"index"`
	Active     bool   `json:"activeDisk"`
	Volume     string `json:"volume"`
	DeviceName string `json:"deviceName"`
	RemainSec  int    `json:"remainingRecordTime"`
	TotalBytes int64  `json:"totalSpace"`
	FreeBytes  int64  `json:"remainingSpace"`
}

func deckREST(host, method, path string, body any, out any) error {
	var last error
	for _, base := range []string{"http://" + host, "https://" + host} {
		var rd io.Reader
		if body != nil { b, _ := json.Marshal(body); rd = bytes.NewReader(b) }
		r, _ := http.NewRequest(method, base+"/control/api/v1"+path, rd)
		if body != nil { r.Header.Set("Content-Type", "application/json") }
		resp, err := camHTTP.Do(r)
		if err != nil { last = err; continue }
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if resp.StatusCode == 404 {
			return fmt.Errorf("a REST API não respondeu (HTTP 404). Precisa do firmware 8.4 ou mais novo e da opção REST API / Web Media Manager ligada no Blackmagic HyperDeck Setup")
		}
		if resp.StatusCode >= 300 {
			msg := strings.TrimSpace(string(data))
			if len(msg) > 160 { msg = msg[:160] }
			return fmt.Errorf("HyperDeck respondeu HTTP %d %s", resp.StatusCode, msg)
		}
		if out != nil && len(data) > 0 { return json.Unmarshal(data, out) }
		return nil
	}
	return fmt.Errorf("sem acesso à REST API do HyperDeck (%v)", last)
}

func deckRESTMedia(host string) ([]DeckMedia, error) {
	var r struct{ Workingset []DeckMedia `json:"workingset"` }
	if err := deckREST(host, "GET", "/media/workingset", nil, &r); err != nil { return nil, err }
	out := []DeckMedia{}
	for _, m := range r.Workingset { if m.DeviceName != "" { out = append(out, m) } }
	return out, nil
}

func deckRESTFormat(host, deviceName, fs, name string) error {
	var k struct{ Key string `json:"key"` }
	if err := deckREST(host, "GET", "/media/devices/"+deviceName+"/doformat", nil, &k); err != nil { return err }
	if k.Key == "" { return fmt.Errorf("o HyperDeck não liberou a formatação desta mídia") }
	if fs == "HFS+" { fs = "HFS+" } else { fs = "ExFAT" }
	return deckREST(host, "PUT", "/media/devices/"+deviceName+"/doformat", map[string]string{"key": k.Key, "volume": name, "filesystem": fs}, nil)
}
