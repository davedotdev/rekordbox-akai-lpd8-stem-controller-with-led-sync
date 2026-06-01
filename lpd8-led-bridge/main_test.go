package main

import (
	"bytes"
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

// The master knob (CC 73 by default) is a hot-path reset: full travel lights
// every pad, zero turns every pad off.
func TestMasterKnobResetsAllPads(t *testing.T) {
	resetState() // default master_knobs includes CC 73

	handleMasterKnob(73, 0) // all off
	for note := range noteToPayloadPos {
		if padState[note] {
			t.Errorf("master knob at zero: pad %d should be off", note)
		}
	}

	handleMasterKnob(73, 127) // all on, full
	for note, pos := range noteToPayloadPos {
		if !padState[note] {
			t.Errorf("master knob full: pad %d should be on", note)
		}
		if padColors[pos] != baseColor(note) {
			t.Errorf("master knob full: pad %d should be full row colour %v, got %v", note, baseColor(note), padColors[pos])
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
