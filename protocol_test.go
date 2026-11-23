package main

import "testing"

func TestCardName(t *testing.T) {
	cases := []struct{ in, fs, want string }{
		{"JOB FINAL 2026 CAM A", "exFAT", "JOB_FINAL_2"},
		{"JOB FINAL 2026 CAM A", "HFS+", "JOB_FINAL_2026_CAM_A"},
		{"Gravação: Nº1/2", "exFAT", "Gravacao_N"},
		{"", "exFAT", ""},
		{"ABCDEFGHIJ_", "exFAT", "ABCDEFGHIJ"},
	}
	for _, c := range cases {
		got := cardName(c.in, c.fs)
		if got != c.want { t.Errorf("cardName(%q, %s) = %q, quero %q", c.in, c.fs, got, c.want) }
		max := 11
		if c.fs == "HFS+" { max = 27 }
		if len(got) > max { t.Errorf("cardName(%q, %s) passou de %d caracteres: %q", c.in, c.fs, max, got) }
	}
}

func TestTimecode(t *testing.T) {
	for _, base := range []int{24, 25, 30, 60} {
		for _, tc := range []string{"00:00:00:00", "01:02:03:04", "23:59:59:" + map[int]string{24: "23", 25: "24", 30: "29", 60: "59"}[base]} {
			f, ok := tcToFrames(tc, base)
			if !ok { t.Fatalf("tcToFrames(%s) falhou", tc) }
			if back := framesToTC(f, base); back != tc { t.Errorf("base %d: %s → %d → %s", base, tc, f, back) }
		}
	}
	if f, ok := tcToFrames("01:00:00;00", 30); !ok || f != 108000 { t.Errorf("drop-frame ';': %d %v", f, ok) }
	if _, ok := tcToFrames("lixo", 30); ok { t.Error("TC inválido aceito") }
	if got := framesToTC(-1, 25); got != "23:59:59:24" { t.Errorf("volta da meia-noite: %s", got) }
	if got := tcSeconds("01:02:03:04"); got != 3723 { t.Errorf("tcSeconds: %d", got) }
}

func TestFps(t *testing.T) {
	cases := map[string]float64{"1080p30": 30, "1080p2997": 29.97, "1080i50": 25, "2160p5994": 59.94, "720p60": 60, "": 0}
	for in, want := range cases {
		if got := fpsOf(in); got != want { t.Errorf("fpsOf(%q) = %v, quero %v", in, got, want) }
	}
	if tcBase(29.97) != 30 || tcBase(0) != 30 || tcBase(25) != 25 { t.Error("tcBase") }
}

func TestParseClipLine(t *testing.T) {
	c, ok := parseClipLine("3: JOB_CAM_A_003 H.264 1080p30 00:12:30:10")
	if !ok || c.ID != 3 || c.Name != "JOB_CAM_A_003" || c.Codec != "H.264" || c.Format != "1080p30" || c.Duration != "00:12:30:10" {
		t.Errorf("disk list: %+v %v", c, ok)
	}
	c, ok = parseClipLine("7: Meu clipe com espaço 01:00:00:00 00:01:00:00")
	if !ok || c.Name != "Meu clipe com espaço" || c.Start != "01:00:00:00" || c.Duration != "00:01:00:00" {
		t.Errorf("clips get: %+v %v", c, ok)
	}
	if _, ok := parseClipLine("sem número"); ok { t.Error("linha inválida aceita") }
}

func TestReelName(t *testing.T) {
	if got := reelName("HyperDeck Cam A"); len(got) > 8 || got == "" { t.Errorf("reelName: %q", got) }
	if got := reelName(""); got != "AX" { t.Errorf("reelName vazio: %q", got) }
}
