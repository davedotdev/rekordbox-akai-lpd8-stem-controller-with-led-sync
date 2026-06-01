# Rekordbox Stem Control with Akai LPD8 MK2

Control **Rekordbox** stems on **two decks** from a single Akai LPD8 MK2 — dedicated pads for stem on/off and knobs for stem isolation — with **RGB LED feedback** that Rekordbox doesn't provide natively.

**Tested with Rekordbox 7.2.14.0323.**

## How it works

One LPD8 drives both decks. The top row of pads/knobs controls Deck 1, the bottom row controls Deck 2:

```
              LPD8 MK2 — one unit, two decks

   K1      K2       K3      K4      ← Deck 1 isolators + reset
  Vocal  Ins/Bass  Drums   RESET
   K5      K6       K7      K8      ← Deck 2 isolators + reset
  Vocal  Ins/Bass  Drums   RESET

  ┌─────┬─────┬─────┬─────┐
  │  5  │  6  │  7  │  8  │   Deck 1 stem on/off
  │ Voc │ Ins │ Bas │ Drm │   (notes 40-43, blue LEDs)
  ├─────┼─────┼─────┼─────┤
  │  1  │  2  │  3  │  4  │   Deck 2 stem on/off
  │ Voc │ Ins │ Bas │ Drm │   (notes 36-39, amber LEDs)
  └─────┴─────┴─────┴─────┘
```

Two pieces wire it up:

1. **The Rekordbox MIDI mapping** (`Rekordbox_midi_import_LPD8_mk2_dual_map.csv`) routes the LPD8's pads and knobs to each deck's stem functions.
2. **The LED bridge** (`rb-lpd8-led-bridge`, in [`lpd8-led-bridge/`](lpd8-led-bridge/)) lights the pads to reflect stem state. It only reads the LPD8's own MIDI, so it avoids feedback loops with Rekordbox.

## Project structure

```
.
├── Rekordbox_midi_import_LPD8_mk2_dual_map.csv  # ← lives in lpd8-led-bridge/
└── lpd8-led-bridge/         # LED feedback program (Go)
    ├── main.go
    ├── config.json
    ├── build.sh
    └── Rekordbox_midi_import_LPD8_mk2_dual_map.csv
```

## Part 1: Rekordbox MIDI mapping

Import [`lpd8-led-bridge/Rekordbox_midi_import_LPD8_mk2_dual_map.csv`](lpd8-led-bridge/Rekordbox_midi_import_LPD8_mk2_dual_map.csv):

1. Connect the LPD8 mk2 and quit any other app using it.
2. In Rekordbox, open **Preferences → Controller → MIDI**.
3. Select **LPD8 mk2** from the device dropdown.
4. Click **Import** and choose the CSV.
5. Make sure MIDI is enabled for the device.

### MIDI assignments

| Control | MIDI | Rekordbox function |
|---------|------|--------------------|
| Pads 5–8 | Notes 40–43 | Deck 1 stem on/off (Vocal, Inst, Bass, Drums) |
| Pads 1–4 | Notes 36–39 | Deck 2 stem on/off (Vocal, Inst, Bass, Drums) |
| Knob 1 | CC 70 | Deck 1 Vocal isolator |
| Knob 2 | CC 71 | Deck 1 Inst isolator |
| Knob 3 | CC 72 | Deck 1 Drums isolator |
| Knob 5 | CC 74 | Deck 2 Vocal isolator |
| Knob 6 | CC 75 | Deck 2 Inst isolator |
| Knob 7 | CC 76 | Deck 2 Drums isolator |

Pads are on **channel 10**, knobs on the **global** channel. Rekordbox exposes Vocal/Inst/Drums isolators (there's no Bass isolator), so the LED bridge lights the Bass pad alongside Inst from the same knob. Knobs 4 and 8 (CC 73 / 77) aren't mapped in Rekordbox — the LED bridge uses them as an all-lights reset.

## Part 2: LED bridge

The LPD8 MK2 has RGB LEDs on each pad, but Rekordbox doesn't drive them. `rb-lpd8-led-bridge` tracks pad presses and knob moves locally and sets the LEDs over SysEx:

- **Top row blue** = Deck 1 stems, **bottom row amber** = Deck 2 stems
- Pads latch on/off; isolator knobs fade each stem's LED brightness
- The **last knob of each row** is a hot-path reset — full = all on, zero = all off — for when the LEDs drift out of sync

See **[lpd8-led-bridge/README.md](lpd8-led-bridge/README.md)** for full documentation: install/run, configuration, building, and troubleshooting.

## LPD8 programming

Program the LPD8 MK2 with Akai's editor so it sends:

| Control | Setting |
|---------|---------|
| Pads 5–8 | Notes 40, 41, 42, 43 |
| Pads 1–4 | Notes 36, 37, 38, 39 |
| Knobs 1–8 | CC 70–77 |
| Pad channel | 10 |
| Knob channel | 1 (global) |

## Requirements

- Akai LPD8 MK2
- Rekordbox (tested with 7.2.14.0323)
- macOS for the prebuilt LED bridge (build from source for Windows — see the bridge README)

## License

MIT License

## Acknowledgments

- [gomidi/midi](https://gitlab.com/gomidi/midi) — Go MIDI library
- [rtmidi](https://github.com/thestk/rtmidi) — cross-platform MIDI I/O
- [lpd8mk2sysex](https://github.com/john-kuan/lpd8mk2sysex) — LPD8 MK2 LED SysEx documentation
