package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"testing"
	"time"
)

func TestVersionNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"2.10", "2.9", true},
		{"2.9", "2.10", false},
		{"2.10.2", "2.10.1", true},
		{"2.10.2", "2.10.2", false},
		{"v2.11", "2.10.2", true},
		{"2.10", "2.10.0", false},
		{"2.11-beta1", "2.10.2", true},
		{"2.11", "2.11-beta1", true},
		{"2.11-beta1", "2.11", false},
		{"2.11-beta10", "2.11-beta2", true},
		{"2.11-rc1", "2.11-beta3", true},
		{"3", "2.99.99", true},
	}
	for _, c := range cases {
		if got := versionNewer(c.a, c.b); got != c.want {
			t.Errorf("versionNewer(%q, %q) = %v, quero %v", c.a, c.b, got, c.want)
		}
	}
}

func TestVerifySums(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	pubB64 := base64.StdEncoding.EncodeToString(pub)
	sums := []byte("abc  RackMonitorByDon_windows.exe\n")
	sig := []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, sums)) + "\n")
	if !verifySums(sums, sig, pubB64) { t.Fatal("assinatura válida recusada") }
	if verifySums([]byte("xyz  RackMonitorByDon_windows.exe\n"), sig, pubB64) { t.Fatal("lista alterada aceita") }
	_, other, _ := ed25519.GenerateKey(nil)
	bad := []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(other, sums)))
	if verifySums(sums, bad, pubB64) { t.Fatal("assinatura de outra chave aceita") }
	if verifySums(sums, []byte("lixo"), pubB64) { t.Fatal("assinatura inválida aceita") }
	if verifySums(sums, sig, "") { t.Fatal("chave vazia aceita") }
}

func TestEmbeddedPubKey(t *testing.T) {
	b, err := base64.StdEncoding.DecodeString(updatePubKey)
	if err != nil || len(b) != ed25519.PublicKeySize { t.Fatalf("updatePubKey inválida: %v (%d bytes)", err, len(b)) }
}

func TestSumFor(t *testing.T) {
	h := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789ABCDEF"
	sums := []byte(h + "  RackMonitorByDon_mac\n" + h[:63] + "0 *RackMonitorByDon_windows.exe\n")
	if got := sumFor(sums, "RackMonitorByDon_mac"); got != "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" {
		t.Errorf("mac: %q", got)
	}
	if got := sumFor(sums, "RackMonitorByDon_windows.exe"); got == "" { t.Error("windows (modo binário *) não encontrado") }
	if got := sumFor(sums, "outro"); got != "" { t.Errorf("outro: %q", got) }
}

func TestNearSchedule(t *testing.T) {
	loc := time.Local
	mon := time.Date(2026, 11, 23, 20, 0, 0, 0, loc) // segunda-feira
	items := []ScheduleItem{{Time: "20:15", Action: "arm"}}
	if !nearSchedule(items, mon) { t.Error("20:00 deveria estar perto de 20:15") }
	if nearSchedule(items, mon.Add(-time.Hour)) { t.Error("19:00 não deveria estar perto de 20:15") }
	if nearSchedule([]ScheduleItem{{Time: "20:15", Days: "67"}}, mon) { t.Error("item só de fim de semana valeu na segunda") }
	if !nearSchedule([]ScheduleItem{{Time: "20:15", Days: "1"}}, mon) { t.Error("item de segunda não valeu na segunda") }
}
