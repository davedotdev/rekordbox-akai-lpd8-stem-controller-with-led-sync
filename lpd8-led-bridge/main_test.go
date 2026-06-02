package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

// The title box must stay aligned: every framed line is the same display width.
func TestBannerBoxAligned(t *testing.T) {
	colorEnabled = false
	var b bytes.Buffer
	printBanner(&b, "0.2", "index 1 (LPD8 mk2)", "config.json", 2)

	width := -1
	for _, ln := range strings.Split(b.String(), "\n") {
		if !strings.ContainsAny(ln, "│┌└") {
			continue
		}
		w := utf8.RuneCountInString(ln)
		if width == -1 {
			width = w
		} else if w != width {
			t.Errorf("box line width %d != %d: %q", w, width, ln)
		}
	}
	if width == -1 {
		t.Fatal("no box lines rendered")
	}
}

// resetState gives each test a clean slate of runtime state.
func resetState() {
	buildMappings(defaultConfig())
	padState = map[uint8]bool{}
	padColors = [8]Color{}
	sendSysEx = func([]byte) error { return nil }
}

// Pad 40 = Deck 1 Vocal, governed by knob CC 70 in the default config.
const (
	testPad = uint8(40)
	testCC  = uint8(70)
)

// When a knob is at zero the stem is off; pressing its pad must NOT light it.
// The knob takes precedence (regression test for the zero-knob lighting bug).
func TestTogglePadIgnoredWhenKnobAtZero(t *testing.T) {
	resetState()

	handleKnobChange(testCC, 127) // knob up -> pad on
	handleKnobChange(testCC, 0)   // knob down -> pad off
	if padState[testPad] {
		t.Fatalf("pad %d should be off after knob to zero", testPad)
	}

	togglePad(testPad) // press while knob is at zero
	if padState[testPad] {
		t.Errorf("pad %d must stay off: knob at zero takes precedence", testPad)
	}
}

// With the knob up, the pad latches normally on each press.
func TestTogglePadLatchesWhenKnobUp(t *testing.T) {
	resetState()

	handleKnobChange(testCC, 127) // knob up -> pad on
	if !padState[testPad] {
		t.Fatalf("pad %d should be on after knob up", testPad)
	}

	togglePad(testPad)
	if padState[testPad] {
		t.Errorf("pad %d should toggle off", testPad)
	}
	togglePad(testPad)
	if !padState[testPad] {
		t.Errorf("pad %d should toggle back on", testPad)
	}
}

// A pad with no knob movement recorded is always pressable (startup all-on).
func TestTogglePadAllowedWhenKnobNeverMoved(t *testing.T) {
	resetState()

	togglePad(testPad)
	if !padState[testPad] {
		t.Errorf("pad %d should toggle on when its knob has never been moved", testPad)
	}
}

// The knob's full-scale value (knobMax, 127 in the default config) must map to
// full LED brightness, with values below it fading linearly. Regression test
// for the "fade reaches only partial brightness" bug.
func TestKnobScalesToFullBrightness(t *testing.T) {
	resetState() // defaultConfig sets KnobMax = 127; pad 40 is blue {0,0,127}
	pos := noteToPayloadPos[testPad]

	handleKnobChange(testCC, 127) // full travel
	if got := padColors[pos].B; got != 127 {
		t.Errorf("at knob max (127) want full brightness 127, got %d", got)
	}

	handleKnobChange(testCC, 64) // ~half travel -> ~half brightness (64*127/127 = 64)
	if got := padColors[pos].B; got != 64 {
		t.Errorf("at half travel want brightness 64, got %d", got)
	}
}

// Reset knobs are deck-scoped: knob 4 (CC 73) drives only the top row (40-43),
// knob 8 (CC 77) only the bottom row (36-39) — not all pads.
func TestMasterKnobIsDeckScoped(t *testing.T) {
	resetState() // default: CC73 -> top row, CC77 -> bottom row

	topRow := map[uint8]bool{40: true, 41: true, 42: true, 43: true}

	// Start with every pad on (tests don't run main's startup init).
	handleMasterKnob(73, 127)
	handleMasterKnob(77, 127)

	// Knob 4 to zero must turn off only the top row; bottom row stays on.
	handleMasterKnob(73, 0)
	for note := range noteToPayloadPos {
		want := !topRow[note] // top off, bottom still on
		if padState[note] != want {
			t.Errorf("knob 4 reset: pad %d on=%v, want %v", note, padState[note], want)
		}
	}

	// Knob 8 to zero now turns off the bottom row too — everything off.
	handleMasterKnob(77, 0)
	for note := range noteToPayloadPos {
		if padState[note] {
			t.Errorf("after both resets to zero: pad %d should be off", note)
		}
	}

	// Knob 4 full lights only the top row, at its blue row colour.
	handleMasterKnob(73, 127)
	for note, pos := range noteToPayloadPos {
		if topRow[note] {
			if !padState[note] || padColors[pos] != baseColor(note) {
				t.Errorf("knob 4 full: top pad %d should be full blue", note)
			}
		} else if padState[note] {
			t.Errorf("knob 4 full: bottom pad %d should be untouched (off)", note)
		}
	}
}

// A v0.2 config with the old array form of master_knobs must still load (not
// crash), with each listed CC resetting all pads.
func TestLegacyMasterKnobsArrayLoads(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/legacy.json"
	cfg := `{
	  "lpd8": { "top_row": [40,41,42,43], "bottom_row": [36,37,38,39],
	            "channel": 10, "knob_channel": 0, "knob_max": 127,
	            "master_knobs": [73, 77] },
	  "knob_to_pad": { "70": [40] }
	}`
	if err := os.WriteFile(path, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := loadConfig(path)
	if err != nil {
		t.Fatalf("legacy array master_knobs should load, got: %v", err)
	}
	buildMappings(loaded)

	for _, cc := range []uint8{73, 77} {
		pads, ok := masterKnobs[cc]
		if !ok {
			t.Errorf("legacy reset knob CC%d missing", cc)
		} else if len(pads) != 8 {
			t.Errorf("legacy reset knob CC%d should cover all 8 pads, got %d", cc, len(pads))
		}
	}
}

func TestScaleColor(t *testing.T) {
	cases := []struct {
		base       Color
		brightness uint8
		want       Color
	}{
		{Color{0, 0, 127}, 127, Color{0, 0, 127}},   // full brightness = base
		{Color{127, 40, 0}, 0, Color{0, 0, 0}},      // zero brightness = off
		{Color{0, 0, 127}, 64, Color{0, 0, 64}},     // half-ish scales linearly
		{Color{127, 40, 0}, 127, Color{127, 40, 0}}, // amber unchanged at full
	}
	for _, c := range cases {
		if got := scaleColor(c.base, c.brightness); got != c.want {
			t.Errorf("scaleColor(%v, %d) = %v, want %v", c.base, c.brightness, got, c.want)
		}
	}
}
