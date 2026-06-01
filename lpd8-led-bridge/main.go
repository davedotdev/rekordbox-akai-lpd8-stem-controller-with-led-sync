package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"unicode/utf8"

	"gitlab.com/gomidi/midi/v2"
	"gitlab.com/gomidi/midi/v2/drivers"
	_ "gitlab.com/gomidi/midi/v2/drivers/rtmididrv"
)

// Config defines the button/knob mappings
type Config struct {
	// LPD8 pad notes (physical layout: top row 5-8, bottom row 1-4)
	LPD8 struct {
		TopRow      [4]int `json:"top_row"`      // Blue pads (default: 40,41,42,43)
		BottomRow   [4]int `json:"bottom_row"`   // Amber pads (default: 36,37,38,39)
		Knobs       [8]int `json:"knobs"`        // CC numbers for knobs 1-8
		Channel     int    `json:"channel"`      // MIDI channel for pads (1-16, default: 10)
		KnobChannel int    `json:"knob_channel"` // MIDI channel for knobs (0=all, 1-16, default: 0)
		KnobMax     int    `json:"knob_max"`     // CC value the knob emits at full travel (maps to full brightness; default 127)
		MasterKnobs []int  `json:"master_knobs"` // CCs that set ALL pad LEDs at once (reset: full=all on, zero=all off)
	} `json:"lpd8"`

	// Device binding: which physical LPD8 this config drives (for multi-deck setups).
	// Two identical LPD8 MK2 units usually enumerate with the SAME port name, so name
	// matching always grabs the first one. OutPortIndex (the position shown by -list,
	// anchored to a fixed USB slot) is the reliable selector for the second unit.
	// Resolution order for the LED output: CLI -out flag > OutPortIndex > OutPort name.
	Device struct {
		OutPort      string `json:"out_port"`       // output port name (substring match)
		OutPortIndex *int   `json:"out_port_index"` // output port index from -list; wins over OutPort when set
	} `json:"device"`

	// Knob to pad mapping: which knob CC drives which pad LED(s).
	// Key is the knob CC number, value is the list of pad notes it drives.
	// One knob can drive several pads (e.g. the Inst+Bass knob drives two).
	// When the knob value is low the pad(s) turn off; otherwise brightness
	// scales with value. The pad keeps its row colour (blue D1 / amber D2).
	KnobToPad map[string][]int `json:"knob_to_pad"`

	// Deprecated: old name for KnobToPad (v0.1 configs). Read as a fallback so
	// renaming the key doesn't silently break existing config files.
	KnobToPadLegacy map[string][]int `json:"knob_to_blue,omitempty"`
}

// Default configuration
func defaultConfig() Config {
	cfg := Config{}
	cfg.LPD8.TopRow = [4]int{40, 41, 42, 43}
	cfg.LPD8.BottomRow = [4]int{36, 37, 38, 39}
	cfg.LPD8.Knobs = [8]int{70, 71, 72, 73, 74, 75, 76, 77}
	cfg.LPD8.Channel = 10
	cfg.LPD8.KnobChannel = 0             // 0 = accept all channels (global)
	cfg.LPD8.KnobMax = 127               // LPD8 knobs emit the full 0-127; lower this only if yours tops out early
	cfg.LPD8.MasterKnobs = []int{73, 77} // last knob of each row = all-lights reset (full=on, zero=off)

	// Default device binding: match the LPD8 by name. For a second unit whose
	// name collides, set device.out_port_index to its position in -list instead.
	cfg.Device.OutPort = "LPD8 mk2"

	// Stem isolator knobs (global channel). Single LPD8, dual deck:
	// top knobs = Deck 1, bottom knobs = Deck 2. Per deck the grouping is
	// Knob 1 = Vocal, Knob 2 = Inst+Bass (two pads), Knob 3 = Drums.
	cfg.KnobToPad = map[string][]int{
		"70": {40},     // Deck 1 Vocal     -> top pad 40
		"71": {41, 42}, // Deck 1 Inst+Bass -> top pads 41,42
		"72": {43},     // Deck 1 Drums     -> top pad 43
		"74": {36},     // Deck 2 Vocal     -> bottom pad 36
		"75": {37, 38}, // Deck 2 Inst+Bass -> bottom pads 37,38
		"76": {39},     // Deck 2 Drums     -> bottom pad 39
	}

	return cfg
}

func loadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}

	// Backward compat: a config with no device binding falls back to the
	// historical name match for a single LPD8.
	if cfg.Device.OutPort == "" && cfg.Device.OutPortIndex == nil {
		cfg.Device.OutPort = "LPD8 mk2"
	}

	// Backward compat: accept the old "knob_to_blue" key as "knob_to_pad".
	if len(cfg.KnobToPad) == 0 && len(cfg.KnobToPadLegacy) > 0 {
		cfg.KnobToPad = cfg.KnobToPadLegacy
		log.Printf("Note: config uses the old 'knob_to_blue' key; rename it to 'knob_to_pad'.")
	}

	return cfg, nil
}

func saveConfig(path string, cfg Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// Build runtime mappings from config
func buildMappings(cfg Config) {
	// Clear and rebuild noteToPayloadPos
	noteToPayloadPos = make(map[uint8]int)
	for i, note := range cfg.LPD8.TopRow {
		noteToPayloadPos[uint8(note)] = i + 4 // Top row = SysEx positions 4-7
	}
	for i, note := range cfg.LPD8.BottomRow {
		noteToPayloadPos[uint8(note)] = i // Bottom row = SysEx positions 0-3
	}

	// Rebuild isTopRow
	isTopRow = make(map[uint8]bool)
	for _, note := range cfg.LPD8.TopRow {
		isTopRow[uint8(note)] = true
	}
	for _, note := range cfg.LPD8.BottomRow {
		isTopRow[uint8(note)] = false
	}

	// Rebuild knobToPad (one CC can drive several pads) and its reverse padToKnob.
	knobToPad = make(map[uint8][]uint8)
	padToKnob = make(map[uint8]uint8)
	knobValue = make(map[uint8]uint8)
	for ccStr, padNotes := range cfg.KnobToPad {
		var cc int
		fmt.Sscanf(ccStr, "%d", &cc)
		pads := make([]uint8, len(padNotes))
		for i, p := range padNotes {
			pads[i] = uint8(p)
			padToKnob[uint8(p)] = uint8(cc)
		}
		knobToPad[uint8(cc)] = pads
	}

	// Store channels (convert 1-16 to 0-15, 0 stays 0 for "all")
	lpd8Channel = uint8(cfg.LPD8.Channel - 1)
	if cfg.LPD8.KnobChannel == 0 {
		lpd8KnobChannel = 255 // Special value meaning "accept all channels"
	} else {
		lpd8KnobChannel = uint8(cfg.LPD8.KnobChannel - 1)
	}

	// Knob full-scale value (maps to full LED brightness). Guard configs that omit it.
	knobMax = cfg.LPD8.KnobMax
	if knobMax <= 0 {
		knobMax = 127
	}

	// Rebuild masterKnobs (CCs that drive every pad at once)
	masterKnobs = make(map[uint8]bool)
	for _, cc := range cfg.LPD8.MasterKnobs {
		masterKnobs[uint8(cc)] = true
	}
}

var lpd8Channel uint8 = 9       // Default channel 10 (0-indexed) for pads
var lpd8KnobChannel uint8 = 255 // Default: accept all channels for knobs
var knobMax int = 127           // Knob CC value at full travel (maps to full brightness)
var debugMode bool = false      // Debug logging

// Version is set at build time via -ldflags "-X main.Version=..." (see build.sh).
var Version = "dev"

// TestedRekordbox is the Rekordbox build this release was verified against.
const TestedRekordbox = "7.2.14.0323"

func debugLog(format string, v ...any) {
	if debugMode {
		log.Printf(format, v...)
	}
}

// LPD8 MK2 SysEx for LED control
// Format: F0 47 7F 4C 06 00 30 [48 bytes] F7
// Product ID = 0x4C (not 0x30)
// Each color channel is 2 bytes: [high=0x00, low=value]
// So each pad = 6 bytes, 8 pads = 48 bytes
var sysExHeader = []byte{0xF0, 0x47, 0x7F, 0x4C, 0x06, 0x00, 0x30}
var sysExFooter = []byte{0xF7}

// Pad colors (RGB values 0-127)
type Color struct {
	R, G, B byte
}

var (
	colorOff       = Color{0, 0, 0}    // LED off (black)
	colorTopRow    = Color{0, 0, 127}  // Blue  for top row    = Deck 1 stem on/off
	colorBottomRow = Color{127, 40, 0} // Amber for bottom row = Deck 2 stem on/off
)

// baseColor returns the full-brightness "on" colour for a pad note,
// chosen by row: blue for the top row (Deck 1), amber for the bottom row (Deck 2).
func baseColor(note uint8) Color {
	if isTopRow[note] {
		return colorTopRow
	}
	return colorBottomRow
}

// scaleColor scales a base colour by brightness (0-127); at 127 it returns the base unchanged.
func scaleColor(base Color, brightness uint8) Color {
	return Color{
		R: byte(int(base.R) * int(brightness) / 127),
		G: byte(int(base.G) * int(brightness) / 127),
		B: byte(int(base.B) * int(brightness) / 127),
	}
}

// knobOnThreshold is the knob value below which the stem is treated as "off".
const knobOnThreshold = 2

// Runtime mappings (rebuilt from config)
var noteToPayloadPos = map[uint8]int{}
var isTopRow = map[uint8]bool{}
var knobToPad = map[uint8][]uint8{} // knob CC -> pad note(s) it drives
var padToKnob = map[uint8]uint8{}   // pad note -> its governing knob CC
var knobValue = map[uint8]uint8{}   // knob CC -> last seen value (absent = never moved)
var masterKnobs = map[uint8]bool{}  // knob CC -> true if it sets all pad LEDs at once

// Current LED colors for each pad position
var padColors [8]Color

// Track toggle state for each pad (true = LED on with color, false = LED off)
var padState = make(map[uint8]bool)
var stateMutex sync.Mutex

// Global send function (set after opening output port)
var sendSysEx func([]byte) error

// Build payload (48 bytes: 6 per pad)
// Each color channel is 2 bytes: [high=0x00, low=value]
func buildPayload(colors [8]Color) []byte {
	payload := make([]byte, 0, 48)
	for _, c := range colors {
		// R: high byte (always 0), low byte (value)
		payload = append(payload, 0x00, c.R)
		// G: high byte (always 0), low byte (value)
		payload = append(payload, 0x00, c.G)
		// B: high byte (always 0), low byte (value)
		payload = append(payload, 0x00, c.B)
	}
	return payload
}

// Build complete SysEx message
func buildSysEx(colors [8]Color) []byte {
	payload := buildPayload(colors)
	msg := make([]byte, 0, 64)
	msg = append(msg, sysExHeader...)
	msg = append(msg, payload...)
	msg = append(msg, sysExFooter...)
	return msg
}

// pushLEDs sends the current padColors to the device. Callers hold stateMutex.
func pushLEDs() {
	if err := sendSysEx(buildSysEx(padColors)); err != nil {
		log.Printf("Error sending SysEx: %v", err)
	}
}

// togglePad flips a pad's on/off LED state and pushes the update.
// If the pad's governing knob is known to be at zero, the press is ignored:
// the knob takes precedence, so a stem whose isolator is fully down stays dark.
func togglePad(note uint8) {
	stateMutex.Lock()
	defer stateMutex.Unlock()

	pos, ok := noteToPayloadPos[note]
	if !ok {
		return
	}

	// Knob takes precedence: ignore the press if its knob is down.
	if cc, governed := padToKnob[note]; governed {
		if v, known := knobValue[cc]; known && v < knobOnThreshold {
			debugLog("Pad %d press ignored: knob CC%d at zero", note, cc)
			return
		}
	}

	padState[note] = !padState[note]
	if padState[note] {
		padColors[pos] = baseColor(note)
	} else {
		padColors[pos] = colorOff
	}

	pushLEDs()
	debugLog("Pad %d toggled -> on=%v", note, padState[note])
}

// Handle knob (CC) change - controls the brightness of one or more stem LEDs.
// Rekordbox stem isolators (global channel). A single knob can drive several
// stem LEDs (e.g. the Inst+Bass knob drives two pads). The LED keeps its row
// colour - blue for Deck 1 (top row), amber for Deck 2 (bottom row).
// value < knobOnThreshold: pad(s) turn off
// otherwise: pad(s) turn on with brightness mapped linearly from the knob value,
// scaling the knob's full-scale output (knobMax) up to full LED brightness (127).
func handleKnobChange(cc uint8, value uint8) {
	padNotes, ok := knobToPad[cc]
	if !ok {
		return
	}

	stateMutex.Lock()
	defer stateMutex.Unlock()

	knobValue[cc] = value // remembered so togglePad can let the knob take precedence

	changed := false
	for _, padNote := range padNotes {
		pos, ok := noteToPayloadPos[padNote]
		if !ok {
			continue
		}

		if value < knobOnThreshold {
			// Turn off
			if !padState[padNote] {
				continue // Already off
			}
			padState[padNote] = false
			padColors[pos] = colorOff
			debugLog("Knob CC%d=%d -> Pad %d OFF", cc, value, padNote)
		} else {
			// Turn on; scale the knob value (0..knobMax) up to full brightness (0..127), keeping the row colour
			brightness := uint8(min(int(value)*127/knobMax, 127))
			padState[padNote] = true
			padColors[pos] = scaleColor(baseColor(padNote), brightness)
			debugLog("Knob CC%d=%d -> Pad %d ON (brightness %d)", cc, value, padNote, brightness)
		}
		changed = true
	}

	if changed {
		pushLEDs()
	}
}

// handleMasterKnob drives every pad LED at once - a hot-path reset for state
// desyncs. Full travel turns all stems on at full brightness, zero turns them
// all off; in between it acts as a master dimmer. Each pad keeps its row colour.
func handleMasterKnob(cc uint8, value uint8) {
	stateMutex.Lock()
	defer stateMutex.Unlock()

	on := value >= knobOnThreshold
	brightness := uint8(min(int(value)*127/knobMax, 127))
	for note, pos := range noteToPayloadPos {
		padState[note] = on
		if on {
			padColors[pos] = scaleColor(baseColor(note), brightness)
		} else {
			padColors[pos] = colorOff
		}
	}

	pushLEDs()
	debugLog("Master knob CC%d=%d -> all pads on=%v (brightness %d)", cc, value, on, brightness)
}

func listPorts() {
	fmt.Println("Available MIDI Input Ports:")
	for i, in := range midi.GetInPorts() {
		fmt.Printf("  [%d] %s\n", i, in)
	}
	fmt.Println("\nAvailable MIDI Output Ports:")
	for i, out := range midi.GetOutPorts() {
		fmt.Printf("  [%d] %s\n", i, out)
	}
}

// ANSI colour, disabled when NO_COLOR is set or stdout isn't a terminal.
const (
	ansiReset = "\x1b[0m"
	ansiDim   = "\x1b[2m"
	ansiBold  = "\x1b[1m"
	ansiCyan  = "\x1b[96m"
	ansiBlue  = "\x1b[94m"
	ansiAmber = "\x1b[38;5;208m"
)

var colorEnabled = true

func initColor() {
	if os.Getenv("NO_COLOR") != "" {
		colorEnabled = false
		return
	}
	if fi, err := os.Stdout.Stat(); err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		colorEnabled = false
	}
}

func paint(code, s string) string {
	if !colorEnabled {
		return s
	}
	return code + s + ansiReset
}

// printBanner renders the startup screen: an ASCII LPD8 showing the deck/stem
// layout, plus the runtime details. Takes an io.Writer so it can be tested.
func printBanner(w io.Writer, version, outDesc, configSrc string, numInputs int) {
	dim := func(s string) string { return paint(ansiDim, s) }

	// Title box (whole line coloured, so padding is measured on plain text).
	const inner = 46
	box := func(text string) string {
		pad := max(inner-utf8.RuneCountInString(text), 0)
		return paint(ansiBold+ansiCyan, "  │"+text+strings.Repeat(" ", pad)+"│")
	}
	rule := func(l, r string) string {
		return paint(ansiBold+ansiCyan, "  "+l+strings.Repeat("─", inner)+r)
	}

	// Knob block: a row of function initials over a row of dots, coloured for the
	// deck. Knob 4/8 (RST) is the all-LEDs reset. Cells are 7 wide so the dot
	// sits centred under its label.
	center := func(s string, width int) string {
		gap := max(width-utf8.RuneCountInString(s), 0)
		left := gap / 2
		return strings.Repeat(" ", left) + s + strings.Repeat(" ", gap-left)
	}
	knobLabels := []string{"V", "I+B", "D", "RST"}
	knobTokens := func(col string) string {
		out := "     "
		for _, tok := range knobLabels {
			out += paint(col, center(tok, 7))
		}
		return out
	}
	knobDots := func(col, label string) string {
		out := "     "
		for range knobLabels {
			out += paint(col, center("●", 7))
		}
		return out + "  " + dim(label)
	}
	// Pad row: four lettered cells coloured for the deck, plus a label.
	padRow := func(col, label string) string {
		cell := func(letter string) string { return "┃ " + paint(col, letter) + " " }
		return "     " + cell("V") + cell("I") + cell("B") + cell("D") + "┃   " + dim(label)
	}
	grid := func(s string) string { return "     " + dim(s) }
	field := func(k, v string) string { return "   " + dim(fmt.Sprintf("%-8s", k)+"→  ") + v }

	fmt.Fprintln(w)
	fmt.Fprintln(w, rule("┌", "┐"))
	fmt.Fprintln(w, box("  rb-lpd8-led-bridge  "+version))
	fmt.Fprintln(w, box("  Rekordbox · Akai LPD8 MK2 · LED sync"))
	fmt.Fprintln(w, rule("└", "┘"))
	fmt.Fprintln(w)
	fmt.Fprintln(w, knobTokens(ansiBlue))
	fmt.Fprintln(w, knobDots(ansiBlue, "knobs 1-4   Deck 1 isolators (4 = reset)"))
	fmt.Fprintln(w, knobTokens(ansiAmber))
	fmt.Fprintln(w, knobDots(ansiAmber, "knobs 5-8   Deck 2 isolators (8 = reset)"))
	fmt.Fprintln(w)
	fmt.Fprintln(w, grid("┏━━━┳━━━┳━━━┳━━━┓"))
	fmt.Fprintln(w, padRow(ansiBlue, "pads 5-8    Deck 1 stems"))
	fmt.Fprintln(w, grid("┣━━━╋━━━╋━━━╋━━━┫"))
	fmt.Fprintln(w, padRow(ansiAmber, "pads 1-4    Deck 2 stems"))
	fmt.Fprintln(w, grid("┗━━━┻━━━┻━━━┻━━━┛"))
	fmt.Fprintln(w, dim("                 V vocal · I inst · B bass · D drums"))
	fmt.Fprintln(w)
	fmt.Fprintln(w, field("output", outDesc))
	fmt.Fprintln(w, field("inputs", fmt.Sprintf("%d MIDI port(s)", numInputs)))
	fmt.Fprintln(w, field("config", configSrc))
	fmt.Fprintln(w, field("reset", "knob 4 / knob 8 — all LEDs full / off"))
	fmt.Fprintln(w, field("tested", "Rekordbox "+TestedRekordbox))
	fmt.Fprintln(w)
	fmt.Fprintln(w, "   "+dim("Ctrl-C to quit"))
	fmt.Fprintln(w)
}

func main() {
	var (
		listOnly    bool
		outputPort  string
		configPath  string
		genConfig   string
		testMode    bool
		outIndex    int
		showVersion bool
	)

	flag.BoolVar(&showVersion, "version", false, "Print version and exit")
	flag.BoolVar(&listOnly, "list", false, "List available MIDI ports and exit")
	flag.StringVar(&outputPort, "out", "", "MIDI output port name (sends to LPD8)")
	flag.IntVar(&outIndex, "out-index", -1, "MIDI output port index from -list (overrides -out; for identifying units)")
	flag.StringVar(&configPath, "config", "", "Path to config file (JSON)")
	flag.StringVar(&genConfig, "genconfig", "", "Generate default config file at path and exit")
	flag.BoolVar(&testMode, "test", false, "Test LED colors and exit")
	flag.BoolVar(&debugMode, "debug", false, "Enable debug logging")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "rb-lpd8-led-bridge %s - Rekordbox LPD8 MK2 LED sync (tested with Rekordbox %s).\n\n", Version, TestedRekordbox)
		fmt.Fprintln(os.Stderr, "Usage: rb-lpd8-led-bridge -out \"LPD8 mk2\" [options]")
		fmt.Fprintln(os.Stderr, "   or: rb-lpd8-led-bridge -config config.json   (output device set via device.* in the config)")
		fmt.Fprintln(os.Stderr, "\nOptions:")
		flag.PrintDefaults()
	}
	flag.Parse()
	initColor()

	if showVersion {
		fmt.Printf("rb-lpd8-led-bridge %s (tested with Rekordbox %s)\n", Version, TestedRekordbox)
		return
	}

	defer midi.CloseDriver()

	// Generate config file if requested
	if genConfig != "" {
		cfg := defaultConfig()
		if err := saveConfig(genConfig, cfg); err != nil {
			log.Fatalf("Failed to write config: %v", err)
		}
		fmt.Printf("Default config written to: %s\n", genConfig)
		return
	}

	// Load config (or use defaults)
	var cfg Config
	configSrc := "built-in defaults"
	if configPath != "" {
		var err error
		cfg, err = loadConfig(configPath)
		if err != nil {
			log.Fatalf("Failed to load config: %v", err)
		}
		configSrc = configPath
	} else {
		cfg = defaultConfig()
	}
	buildMappings(cfg)

	if listOnly {
		listPorts()
		return
	}

	// Resolve which physical LPD8 to drive its LEDs.
	// Order: CLI -out-index > CLI -out (name) > config device.out_port_index > config device.out_port (name).
	// Index selection is what distinguishes two identical units that share a port name.
	var outPort drivers.Out
	var outDesc string
	var err error
	switch {
	case outIndex >= 0:
		ports := midi.GetOutPorts()
		if outIndex >= len(ports) {
			log.Fatalf("-out-index %d is out of range (%d output ports found). Run -list to check.", outIndex, len(ports))
		}
		outPort = ports[outIndex]
		outDesc = fmt.Sprintf("index %d (%s, via -out-index)", outIndex, outPort.String())
	case outputPort != "":
		outPort, err = midi.FindOutPort(outputPort)
		outDesc = fmt.Sprintf("%q (CLI name match)", outputPort)
	case cfg.Device.OutPortIndex != nil:
		idx := *cfg.Device.OutPortIndex
		ports := midi.GetOutPorts()
		if idx < 0 || idx >= len(ports) {
			log.Fatalf("device.out_port_index %d is out of range (%d output ports found). Run -list to check.", idx, len(ports))
		}
		outPort = ports[idx]
		outDesc = fmt.Sprintf("index %d (%s)", idx, outPort.String())
	case cfg.Device.OutPort != "":
		outPort, err = midi.FindOutPort(cfg.Device.OutPort)
		outDesc = fmt.Sprintf("%q (config name match)", cfg.Device.OutPort)
	default:
		fmt.Fprintln(os.Stderr, "No LPD8 output port set. Use -out / -out-index, or device.out_port[_index] in a -config file.")
		fmt.Fprintln(os.Stderr)
		flag.Usage()
		fmt.Fprintln(os.Stderr)
		listPorts()
		os.Exit(1)
	}
	if err != nil {
		log.Fatalf("Output port not found: %s (%v)", outDesc, err)
	}

	// Create send function using the output port
	send, err := midi.SendTo(outPort)
	if err != nil {
		log.Fatalf("Failed to open output port: %v", err)
	}

	// Set the global send function for SysEx
	sendSysEx = func(data []byte) error {
		return send(data)
	}

	// Test mode - cycle through colors
	if testMode {
		log.Println("Test mode: cycling LED colors...")
		log.Println("Format: F0 47 7F 4C 06 00 30 [48 bytes] F7")

		testColors := []struct {
			name  string
			color Color
		}{
			{"RED", Color{127, 0, 0}},
			{"GREEN", Color{0, 127, 0}},
			{"BLUE", Color{0, 0, 127}},
			{"WHITE", Color{127, 127, 127}},
			{"OFF", Color{0, 0, 0}},
		}

		for _, tc := range testColors {
			var colors [8]Color
			for i := range colors {
				colors[i] = tc.color
			}

			sysex := buildSysEx(colors)
			fmt.Printf("\n%s - Sending %d bytes: % X\n", tc.name, len(sysex), sysex)

			if err := sendSysEx(sysex); err != nil {
				fmt.Printf("Error: %v\n", err)
			} else {
				fmt.Println("Sent!")
			}

			fmt.Print("Press Enter for next color...")
			fmt.Scanln()
		}

		log.Println("Test complete")
		return
	}

	// Initialize pad states and LED colors from config.
	// All stems start ON: top row = Deck 1 (blue), bottom row = Deck 2 (amber).
	for _, note := range cfg.LPD8.TopRow {
		n := uint8(note)
		padState[n] = true // Deck 1 stem ON
		pos := noteToPayloadPos[n]
		padColors[pos] = colorTopRow // Blue
	}
	for _, note := range cfg.LPD8.BottomRow {
		n := uint8(note)
		padState[n] = true // Deck 2 stem ON
		pos := noteToPayloadPos[n]
		padColors[pos] = colorBottomRow // Amber
	}

	pushLEDs()

	// processPadPress latches a pad note independently: top row (40-43) =
	// Deck 1 (blue), bottom row (36-39) = Deck 2 (amber). togglePad picks the
	// colour from the row and respects the governing knob (see togglePad).
	processPadPress := func(note uint8) {
		if _, ok := noteToPayloadPos[note]; ok {
			debugLog("pad press: note=%d", note)
			togglePad(note)
		}
	}

	// MIDI message handler for LPD8
	handler := func(msg midi.Message, timestampms int32) {
		var ch, key, val uint8

		// Raw trace of everything received (helps diagnose unexpected notes/CCs).
		debugLog("MIDI in: %-28s [% X]", msg.String(), []byte(msg))

		switch {
		case msg.GetNoteOn(&ch, &key, &val):
			// Only respond to configured channel and actual pad presses (vel > 0)
			if ch == lpd8Channel && val > 0 {
				processPadPress(key)
			}
		case msg.GetControlChange(&ch, &key, &val):
			// Handle knob (CC) changes - accept configured channel or all (255)
			if lpd8KnobChannel == 255 || ch == lpd8KnobChannel {
				if masterKnobs[key] {
					handleMasterKnob(key, val)
				} else {
					handleKnobChange(key, val)
				}
			}
		}
	}

	var stopFuncs []func()

	// Listen to all MIDI inputs for LPD8 pad presses and knob moves
	inPorts := midi.GetInPorts()
	for _, inPort := range inPorts {
		stop, err := midi.ListenTo(inPort, handler)
		if err != nil {
			log.Printf("Warning: couldn't listen to %s: %v", inPort, err)
			continue
		}
		stopFuncs = append(stopFuncs, stop)
		debugLog("Listening on: %s", inPort)
	}

	if len(stopFuncs) == 0 {
		log.Println("WARNING: No MIDI input ports found!")
	}

	printBanner(os.Stdout, Version, outDesc, configSrc, len(stopFuncs))

	// Wait for interrupt
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	for _, stop := range stopFuncs {
		stop()
	}
	log.Println("Shutting down...")
}
