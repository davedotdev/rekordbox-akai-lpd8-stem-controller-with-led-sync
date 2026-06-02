# Rekordbox Stem Control with Akai LPD8 MK2

Control **Rekordbox** stems on **two decks** from a single Akai LPD8 MK2 — dedicated pads for stem on/off and knobs for stem isolation — with **RGB LED feedback** that Rekordbox doesn't provide natively.

Watch here >

[![Quick overview!](https://img.youtube.com/vi/2Ip9hci9EY4/0.jpg)](https://www.youtube.com/watch?v=2Ip9hci9EY4)

**Tested with Rekordbox 7.2.14.0323.** (Run `rb-lpd8-led-bridge -version` to see the build and tested Rekordbox version.)

Two pieces wire it up:

1. **The Rekordbox MIDI mapping** ([`lpd8-led-bridge/Rekordbox_midi_import_LPD8_mk2_dual_map.csv`](lpd8-led-bridge/Rekordbox_midi_import_LPD8_mk2_dual_map.csv)) routes the LPD8's pads and knobs to each deck's stem functions.
2. **The LED bridge** (`rb-lpd8-led-bridge`, a Go program in [`lpd8-led-bridge/`](lpd8-led-bridge/)) lights the pads to reflect stem state. It only reads the LPD8's own MIDI, so it avoids feedback loops with Rekordbox.

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

## How It Works

```
┌─────────────┐     ┌──────────────────┐     ┌─────────────┐
│   LPD8 MK2  │────▶│ rb-lpd8-led-bridge│───▶│  LPD8 LEDs  │
│ (pads/knobs)│     │   Local State    │     │  (SysEx)    │
└─────────────┘     │    Tracking      │     └─────────────┘
                    └──────────────────┘
```

### Local State Tracking

This bridge **does not intercept MIDI messages from Rekordbox**. Instead, it tracks pad presses and knob moves locally:

- When you press a pad (or turn a knob) on the LPD8, the bridge updates the LED state
- Rekordbox receives the same MIDI, so the LEDs and Rekordbox stay in sync

**What this means:**
- If you change stems **from within Rekordbox's UI** (mouse/keyboard), the LPD8 LEDs won't update
- If the LEDs ever drift out of sync, use the **master/reset knob** (see [LED Behavior](#led-behavior)) or **restart the bridge**

This design avoids MIDI feedback loops and works reliably with Rekordbox's MIDI implementation.

## Project structure

```
.
├── video/                   # demo / overview footage
└── lpd8-led-bridge/         # LED feedback program (Go) + Rekordbox assets
    ├── main.go
    ├── config.json
    ├── build.sh
    ├── notarize.sh
    └── Rekordbox_midi_import_LPD8_mk2_dual_map.csv
```

## Part 1: Rekordbox MIDI mapping

One LPD8 controls stems on **both decks**. The Rekordbox MIDI mapping routes pads/knobs to each deck's stem functions; the bridge (Part 2) drives the LED feedback. Rekordbox does not light the pads itself.

### Import the MIDI mapping

[`lpd8-led-bridge/Rekordbox_midi_import_LPD8_mk2_dual_map.csv`](lpd8-led-bridge/Rekordbox_midi_import_LPD8_mk2_dual_map.csv) maps the LPD8 to both decks:

| Control | Maps to |
|---------|---------|
| Top pads (notes 40–43) | **Deck 1** stem on/off — Vocal, Inst, Bass, Drums |
| Bottom pads (notes 36–39) | **Deck 2** stem on/off — Vocal, Inst, Bass, Drums |
| Top knobs (CC 70–72) | **Deck 1** stem isolators — Vocal, Inst, Drums |
| Bottom knobs (CC 74–76) | **Deck 2** stem isolators — Vocal, Inst, Drums |

To import it:

1. Connect the LPD8 mk2 and quit any other app using it.
2. In Rekordbox, open **Preferences → Controller → MIDI**.
3. Select **LPD8 mk2** from the device dropdown.
4. Click **Import** and choose `Rekordbox_midi_import_LPD8_mk2_dual_map.csv`.
5. Make sure MIDI is enabled for the device.

Rekordbox stores MIDI mappings as CSV and exposes **Import/Export** on this screen, so you can re-import this file any time the mapping gets reset.

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

### LPD8 programming

Program the LPD8 MK2 with Akai's editor so it sends pads on notes 36–43 / channel 10 and knobs on CC 70–77 / global channel — that's what the mapping above expects:

| Control | Setting |
|---------|---------|
| Pads 5–8 | Notes 40, 41, 42, 43 |
| Pads 1–4 | Notes 36, 37, 38, 39 |
| Knobs 1–8 | CC 70–77 |
| Pad channel | 10 |
| Knob channel | 1 (global) |

## Part 2: LED bridge

The LPD8 MK2 has RGB LEDs on each pad, but Rekordbox doesn't drive them. `rb-lpd8-led-bridge` tracks pad presses and knob moves locally and sets the LEDs over SysEx:

- **Top row blue** = Deck 1 stems, **bottom row amber** = Deck 2 stems
- Pads latch on/off; isolator knobs fade each stem's LED brightness
- The **last knob of each row** is a hot-path reset — full = all on, zero = all off — for when the LEDs drift out of sync

### Installation

#### Pre-built Binaries

Download the `.zip` for your Mac from the project's **GitHub Releases** page and unzip it:

- `rb-lpd8-led-bridge-darwin-arm64.zip` — macOS Apple Silicon
- `rb-lpd8-led-bridge-darwin-amd64.zip` — macOS Intel

The binaries are signed and notarized by Apple, so they run without the "unidentified developer" warning (the first launch does a quick online Gatekeeper check). Then make it executable: `chmod +x rb-lpd8-led-bridge-darwin-arm64`.

**Windows:** a `rb-lpd8-led-bridge-windows-amd64.exe` may be attached to a release (built per [Building Releases](#building-releases)). It is **not code-signed yet** (signing pending), so Windows SmartScreen will warn — click *More info → Run anyway*. You can also build it yourself (`./build.sh <version> windows`, or run `build.sh` on a Windows machine).

#### From Source

**Requirements:**
- Go 1.22 or later
- C compiler (for rtmidi CGO dependencies)
  - macOS: Xcode Command Line Tools (`xcode-select --install`)
  - Windows: MinGW-w64 or MSYS2
  - Linux: `build-essential` package

```bash
cd lpd8-led-bridge
go build -o rb-lpd8-led-bridge .

# Or use the build script (sets the version string)
./build.sh 0.1
```

### Run the LED bridge

```bash
# from the lpd8-led-bridge folder, after building (see Installation)
./rb-lpd8-led-bridge -config config.json
```

At startup all stems are lit — **Deck 1 blue (top), Deck 2 amber (bottom)** — and the bridge then tracks every pad press and knob turn locally. Turn a stem's isolator knob fully down and that pad goes dark; the knob takes precedence, so pressing the pad won't relight it until you bring the knob back up.

If the LEDs don't respond, the LPD8 may expose more than one MIDI port — see [One LPD8, Two Decks](#one-lpd8-two-decks) to pin the right one with `-out-index`.

### Usage

```bash
# List available MIDI ports
./rb-lpd8-led-bridge -list

# Run with the bundled config
./rb-lpd8-led-bridge -config config.json

# Or point at the LPD8 by name directly
./rb-lpd8-led-bridge -out "LPD8 mk2"
```

#### Command Line Options

| Option | Description |
|--------|-------------|
| `-config FILE` | Load configuration from JSON file |
| `-out "PORT"` | MIDI output port name for LPD8 (overrides config) |
| `-out-index N` | MIDI output port **index** from `-list` (overrides `-out`; for identifying units) |
| `-genconfig FILE` | Generate default config file and exit |
| `-list` | List available MIDI ports |
| `-test` | Test LED colors |
| `-version` | Print version (and tested Rekordbox build) and exit |
| `-debug` | Enable verbose debug logging |

The output device can also be set inside the config file (`device.out_port` / `device.out_port_index`), so the config is self-contained. Resolution order: `-out-index` → `-out` → `device.out_port_index` → `device.out_port`.

## One LPD8, Two Decks

Rekordbox can't distinguish two identical LPD8s, so a **single** LPD8 controls both decks: the top pad/knob rows are Deck 1, the bottom rows are Deck 2 (see [LED Behavior](#led-behavior)). Rekordbox routes each deck by note/CC, and the bridge just tracks all 8 pad LEDs plus the six isolator knobs.

```bash
./rb-lpd8-led-bridge -config config.json
```

> ⚠️ A single LPD8 MK2 can expose **more than one MIDI port** (e.g. `-list` may show two entries both named `LPD8 mk2`). If the LEDs don't respond, find the port that actually drives them and pin it in the config:
>
> ```bash
> ./rb-lpd8-led-bridge -list              # note the output port indices
> ./rb-lpd8-led-bridge -out-index 0 -test # which index lights the pads?
> ./rb-lpd8-led-bridge -out-index 1 -test
> ```
>
> Then set `device.out_port_index` in `config.json` to the index that worked.

## LED Behavior

One LPD8 drives **both decks**. The pad and knob rows are split: top = Deck 1, bottom = Deck 2.

```
KNOBS   K1     K2     K3     K4        ← Top knobs    = Deck 1 stem ISO/fade
        K5     K6     K7     K8        ← Bottom knobs = Deck 2 stem ISO/fade
        (per deck: K1=Vocal, K2=Inst+Bass, K3=Drums, K4 unused)

PADS  ┌─────┬─────┬─────┬─────┐
      │ Voc │ Ins │ Bas │ Drm │  ← Top row (Blue)  = Deck 1 stem On/Off
      │ NT40│ NT41│ NT42│ NT43│
      ├─────┼─────┼─────┼─────┤
      │ Voc │ Ins │ Bas │ Drm │  ← Bottom row (Amber) = Deck 2 stem On/Off
      │ NT36│ NT37│ NT38│ NT39│
      └─────┴─────┴─────┴─────┘
```

| Action | Result |
|--------|--------|
| **Startup** | All stems ON — Deck 1 (top) blue, Deck 2 (bottom) amber |
| **Press a pad** | Toggle that stem's on/off LED, in its deck colour (independent latch) |
| **Knob to 0** | The stem LED(s) that knob drives turn OFF |
| **Knob above 2** | Those stem LED(s) turn ON, brightness scales with value, keeping the deck colour |
| **Reset knob (last of each row)** | Knob 4 (CC 73) resets the **top row (Deck 1)**, knob 8 (CC 77) the **bottom row (Deck 2)** — full = that deck's stems on, zero = off. A hot-path resync if the LEDs drift. |

> The master knobs (knob 4 / knob 8) aren't mapped in Rekordbox, so they only affect the bridge's LEDs — handy as a dedicated resync that doesn't touch the audio.

Each pad latches independently. The knob keeps the pad's row colour (blue for Deck 1, amber for Deck 2) — turning a Deck 2 knob dims/brightens its amber pad, it does not turn it blue.

### Knob → Stem LED Mappings (per deck)

Three isolator knobs cover four stems; the middle knob drives the two middle pads:

| Knob | Stem(s) | Deck 1 (top, CC) → pads | Deck 2 (bottom, CC) → pads |
|------|---------|--------------------------|-----------------------------|
| Knob 1 | Vocal | CC 70 → 40 | CC 74 → 36 |
| Knob 2 | Inst + Bass | CC 71 → 41, 42 | CC 75 → 37, 38 |
| Knob 3 | Drums | CC 72 → 43 | CC 76 → 39 |

Knobs 4 and 8 (CC 73, CC 77) are unused.

## Configuration

Generate a default config with `-genconfig config.json`:

```json
{
  "lpd8": {
    "top_row": [40, 41, 42, 43],
    "bottom_row": [36, 37, 38, 39],
    "knobs": [70, 71, 72, 73, 74, 75, 76, 77],
    "channel": 10,
    "knob_channel": 0,
    "knob_max": 127,
    "master_knobs": {
      "73": [40, 41, 42, 43],
      "77": [36, 37, 38, 39]
    }
  },
  "device": {
    "out_port": "LPD8 mk2",
    "out_port_index": null
  },
  "knob_to_pad": {
    "70": [40], "71": [41, 42], "72": [43],
    "74": [36], "75": [37, 38], "76": [39]
  }
}
```

### Config Fields

| Field | Description |
|-------|-------------|
| `lpd8.top_row` | MIDI notes for top row pads (blue LEDs) |
| `lpd8.bottom_row` | MIDI notes for bottom row pads (amber LEDs) |
| `lpd8.knobs` | CC numbers for knobs 1-8 |
| `lpd8.channel` | MIDI channel for pads (1-16) |
| `lpd8.knob_channel` | MIDI channel for knobs (0 = all channels) |
| `lpd8.knob_max` | CC value the knob emits at full travel — mapped to full LED brightness. LPD8 knobs send the full 0–127, so the default is 127; lower it only if your knob tops out early (find its max with `-debug`). |
| `lpd8.master_knobs` | Reset knobs: each CC → the pad notes it drives together (full = those stems on, zero = off, in between = dimmer). Default scopes knob 4 (CC 73) to the top row and knob 8 (CC 77) to the bottom row, so each resets one deck. |
| `device.out_port` | Output port name to match (substring); used when `out_port_index` is null |
| `device.out_port_index` | Output port index from `-list` (anchored to a USB slot); distinguishes identical units |
| `knob_to_pad` | Which pad LED(s) each knob CC drives (list — one knob can drive several) |

## Troubleshooting

### LEDs out of sync with Rekordbox

Restart the bridge to reset to default state:
```bash
# Ctrl+C to stop, then restart
./rb-lpd8-led-bridge -out "LPD8 mk2"
```

### No MIDI ports found

- Ensure the LPD8 is connected and powered on
- Check that no other application has exclusive access to the MIDI port
- On macOS, you may need to enable the IAC Driver in Audio MIDI Setup

### LEDs not responding

- Verify the port name matches exactly (use `-list` to check)
- Test with `-test` to cycle through colors
- Ensure your LPD8 is in the correct program/preset

### Wrong pads lighting up

- The LPD8's pad notes may differ from defaults if reprogrammed
- Use a MIDI monitor to check what notes your LPD8 sends
- Update the config file to match your LPD8's programming

### Knob fade only reaches partial (or jumps to full) brightness

The brightness scales the knob's full-travel value (`lpd8.knob_max`) up to full LED brightness. If the fade tops out dim, your knob's max is **lower** than `knob_max`; if it slams to full early, it's **higher**. Find the real value:

```bash
./rb-lpd8-led-bridge -config config.json -debug
# turn a stem knob fully up and read the highest "Knob CC..=N" value
```

Set `lpd8.knob_max` to that `N` and restart — no rebuild needed.

### Debugging

```bash
./rb-lpd8-led-bridge -out "LPD8 mk2" -debug
```

This shows verbose logging of pad presses, knob changes, and LED state changes.

## Building Releases

rtmidi uses CGO, so each target needs a matching C/C++ toolchain. Run these from the `lpd8-led-bridge/` folder.

```bash
# macOS: builds BOTH darwin/arm64 and darwin/amd64 and stamps the version
./build.sh 0.3
# Creates: releases/rb-lpd8-led-bridge-darwin-arm64
#          releases/rb-lpd8-led-bridge-darwin-amd64

# Windows amd64, cross-compiled from macOS/Linux (needs mingw-w64):
brew install mingw-w64           # macOS  (Debian: apt install gcc-mingw-w64)
./build.sh 0.3 windows
# Creates: releases/rb-lpd8-led-bridge-windows-amd64.exe
#
# Or just run ./build.sh 0.3 on a Windows machine.
```

> **Windows code signing is pending.** The Windows `.exe` is not yet signed, so
> SmartScreen / "Windows protected your PC" warnings are expected — users click
> *More info → Run anyway*. Authenticode signing + reputation is planned for a
> future release. (macOS builds are signed and notarized — see below.)

### Notarizing the macOS build (maintainer)

**Note: The macOS versions are notarized already in the releases. No security warnings etc will bug you. Not the case for Windows. That will be done later.**

So macOS users can run the download without the "unidentified developer" warning, sign and notarize the Mac binaries with `notarize.sh`. Requires an Apple Developer account and a **Developer ID Application** certificate.

One-time: store an Apple credential in a keychain profile (uses an [app-specific password](https://account.apple.com) → Sign-In & Security):

```bash
xcrun notarytool store-credentials rb-lpd8-notary \
    --apple-id "you@example.com" --team-id "TEAMID" --password "app-specific-password"
```

Then, after `build.sh`:

```bash
./notarize.sh          # signs (hardened runtime + timestamp), zips, submits to Apple, waits
# Produces: releases/rb-lpd8-led-bridge-darwin-arm64.zip
#           releases/rb-lpd8-led-bridge-darwin-amd64.zip
```

A bare CLI binary can't be *stapled* (only `.app`/`.dmg`/`.pkg` hold a ticket), so Gatekeeper verifies the notarization online the first time the binary runs — no `xattr` workaround needed.

### Publishing

`releases/` is git-ignored — upload the **notarized `.zip`s** as GitHub Release assets:
```bash
gh release create v0.2 releases/rb-lpd8-led-bridge-darwin-*.zip --title "v0.2" --notes "..."
```

## Requirements

- Akai LPD8 MK2
- Rekordbox (tested with 7.2.14.0323)
- macOS for the prebuilt LED bridge (build from source for Windows — see [Building Releases](#building-releases))

## License

MIT License

## Acknowledgments

- [gomidi/midi](https://gitlab.com/gomidi/midi) — Go MIDI library
- [rtmidi](https://github.com/thestk/rtmidi) — cross-platform MIDI I/O
- [lpd8mk2sysex](https://github.com/john-kuan/lpd8mk2sysex) — LPD8 MK2 LED SysEx documentation
