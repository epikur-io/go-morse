# go-morse

A Morse code encoder, decoder, and WAV audio generator for Go, compliant with **ITU-R M.1677-1**.

Can be used as a **Go library** or as a **command-line tool**.

- Encode plain text → Morse code
- Decode Morse code → plain text
- Render plain text or Morse code → WAV audio file
- Multi-language: Latin/ITU, German, Russian (Cyrillic), Chinese (CTC)
- Custom translation tables via JSON
- Immutable, thread-safe code tables
- Proper Unicode normalisation (NFC) for accented and decomposed characters

---

## Table of Contents

- [Installation](#installation)
- [Library Usage](#library-usage)
  - [Encode and Decode](#encode-and-decode)
  - [Stream I/O](#stream-io)
  - [WAV Generation](#wav-generation)
  - [Language Selection](#language-selection)
  - [Custom Tables](#custom-tables)
  - [API Reference](#api-reference)
- [CLI Usage](#cli-usage)
  - [encode](#encode)
  - [decode](#decode)
  - [wav](#wav)
  - [langs](#langs)
  - [showtable](#showtable)
- [Language Support](#language-support)
- [Custom Table Format](#custom-table-format)
- [WAV Audio Details](#wav-audio-details)
- [Supported Characters](#supported-characters)
- [Project Structure](#project-structure)
- [Development](#development)
- [License](#license)

---

## Installation

**Requirements:** Go 1.21 or later.

### As a library

```bash
go get github.com/epikur-io/go-morse
```

### As a CLI tool

```bash
go install github.com/epikur-io/go-morse/cmd/morse@latest
```

Or build from source:

```bash
git clone https://github.com/epikur-io/go-morse.git
cd go-morse
go build -o morse ./cmd/morse
```

---

## Library Usage

### Encode and Decode

The central type is `*CodeTable`. Obtain one via `ResolveTable`, then call
`EncodeLine` or `DecodeLine` on individual strings.

```go
import morse "github.com/epikur-io/go-morse"

// Obtain the default ITU table (no custom file).
ct, err := morse.ResolveTable("itu", "")
if err != nil {
    log.Fatal(err)
}

// Encode a line of plain text → Morse.
code, err := ct.EncodeLine("HELLO WORLD")
// code == ".... . .-.. .-.. --- / .-- --- .-. .-.. -.."

// Decode a line of Morse → plain text.
text, err := ct.DecodeLine("... --- ...")
// text == "SOS"
```

`EncodeLine` and `DecodeLine` are safe to call concurrently on the same
`*CodeTable`; the table is immutable after construction.

### Stream I/O

For multi-line input use `Encode` and `Decode`, which read from an
`io.Reader` and write to an `io.Writer` line by line.

```go
ct, _ := morse.ResolveTable("itu", "")

// Encode several lines at once.
input := strings.NewReader("SOS\nHELLO WORLD\n")
var output strings.Builder
if err := ct.Encode(input, &output); err != nil {
    log.Fatal(err)
}
fmt.Print(output.String())
// ... --- ...
// .... . .-.. .-.. --- / .-- --- .-. .-.. -..

// Decode from a file, write to stdout.
f, _ := os.Open("message.morse")
defer f.Close()
if err := ct.Decode(f, os.Stdout); err != nil {
    log.Fatal(err)
}
```

### WAV Generation

`GenerateWAV` renders plain text or pre-encoded Morse into a 16-bit mono
PCM WAV file. Pass any `io.Writer` — when the writer also implements
`io.WriteSeeker` (e.g. `*os.File`) the WAV is written directly; otherwise a
temporary file is used transparently.

```go
ct, _ := morse.ResolveTable("itu", "")

p := morse.WavParams{
    FreqHz:     700,   // tone frequency in Hz
    DotMs:      60,    // dot duration in ms (~20 WPM)
    Amplitude:  0.8,   // 0.0–1.0
    SampleRate: 44100, // PCM sample rate
    FadeMs:     5,     // cosine rise/fall fade to eliminate key-click
    InputMode:  "text",// "text" (encode first) or "morse" (raw dots/dashes)
}

// Validate parameters before rendering.
if err := morse.ValidateWavParams(p); err != nil {
    log.Fatal(err)
}

// Write to a file.
f, _ := os.Create("sos.wav")
defer f.Close()
if err := morse.GenerateWAV(strings.NewReader("SOS"), f, p, ct); err != nil {
    log.Fatal(err)
}

// Or write to any io.Writer (e.g. an HTTP response).
morse.GenerateWAV(strings.NewReader("SOS"), w, p, ct)
```

To render pre-encoded Morse instead of plain text, set `InputMode: "morse"`:

```go
p.InputMode = "morse"
morse.GenerateWAV(strings.NewReader("... --- ..."), f, p, ct)
```

### Language Selection

Pass a language ID to `ResolveTable`:

```go
// German — includes Ä, Ö, Ü, ß, and the SCH trigraph.
ct, _ := morse.ResolveTable("de", "")
code, _ := ct.EncodeLine("SCHÖNE GRÜßE")

// Russian / Cyrillic.
ct, _ = morse.ResolveTable("ru", "")
code, _ = ct.EncodeLine("МОСКВА")

// Chinese — digits and Latin only; use CTC 4-digit codes for Hanzi.
ct, _ = morse.ResolveTable("zh", "")
code, _ = ct.EncodeLine("4919 1072") // 你(4919) 好(1072)
```

Available IDs: `itu`, `en`, `de`, `ru`, `zh`. See [Language Support](#language-support).

### Custom Tables

Extend any language table with a JSON file via the second argument to
`ResolveTable`:

```go
// Merge extras.json on top of the ITU table.
ct, err := morse.ResolveTable("itu", "extras.json")
if err != nil {
    log.Fatal(err)
}
code, _ := ct.EncodeLine("#") // uses your custom mapping
```

For lower-level control, use `LookupLang` and `MergeCustomTable` directly
and then seal the result with `BuildCodeTable`:

```go
spec, err := morse.LookupLang("de")
if err != nil {
    log.Fatal(err)
}
spec, err = morse.MergeCustomTable(spec, "my_additions.json")
if err != nil {
    log.Fatal(err)
}
ct := morse.BuildCodeTable(spec)
// ct is now a sealed, immutable *CodeTable.
```

`LookupLang` and `MergeCustomTable` never mutate their inputs — each returns
a freshly deep-copied spec.

### API Reference

#### Types

| Type | Description |
|------|-------------|
| `CodeTable` | Immutable encode/decode table. Safe for concurrent use. |
| `WavParams` | Parameters for WAV audio generation. |

#### Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `ResolveTable` | `(langID, tableFile string) (*CodeTable, error)` | Build a table from a language ID and optional JSON override file. The primary entry point for most users. |
| `LookupLang` | `(id string) (tableSpec, error)` | Return the raw spec for a language. Accepts any casing. |
| `MergeCustomTable` | `(spec tableSpec, path string) (tableSpec, error)` | Merge a JSON file into a deep copy of spec. Never mutates its input. |
| `BuildCodeTable` | `(spec tableSpec) *CodeTable` | Seal a spec into an immutable `*CodeTable`. |
| `ValidateWavParams` | `(p WavParams) error` | Validate all WAV parameters, returning a combined error for every violation. |
| `GenerateWAV` | `(r io.Reader, w io.Writer, p WavParams, ct *CodeTable) error` | Render plain text or Morse code to a WAV stream. |
| `PrintLangs` | `(w io.Writer)` | Write the list of built-in languages to w. |
| `PrintTableContents` | `(w io.Writer, ct *CodeTable, langID string)` | Dump a full encode/decode table to w. |

#### `*CodeTable` methods

| Method | Signature | Description |
|--------|-----------|-------------|
| `EncodeLine` | `(line string) (string, error)` | Encode one line of plain text to Morse. |
| `DecodeLine` | `(line string) (string, error)` | Decode one line of Morse to plain text. Accepts flexible `/` separators. |
| `Encode` | `(r io.Reader, w io.Writer) error` | Stream-encode: read plain text line by line, write Morse. |
| `Decode` | `(r io.Reader, w io.Writer) error` | Stream-decode: read Morse line by line, write plain text. |

#### `WavParams` fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `FreqHz` | `float64` | `700` | Tone frequency in Hz |
| `DotMs` | `int` | `60` | Dot duration in ms (WPM ≈ 1200 ÷ DotMs) |
| `Amplitude` | `float64` | `0.8` | Output amplitude, 0.0–1.0 |
| `SampleRate` | `int` | `44100` | PCM sample rate in Hz (min 8000) |
| `FadeMs` | `int` | `5` | Cosine rise/fall time in ms (0 = hard edges) |
| `InputMode` | `string` | `"text"` | `"text"` or `"morse"` |

---

## CLI Usage

```
morse <command> [flags]
```

| Command | Description |
|---------|-------------|
| `encode` | Read plain text, write Morse code |
| `decode` | Read Morse code, write plain text |
| `wav` | Read plain text or Morse code, write WAV audio |
| `langs` | List all built-in language tables |
| `showtable` | Dump the encode/decode table for a language |

When `-f` is omitted, the tool reads from **stdin**.
When `-o` is omitted for `wav`, it writes to **stdout**.

### encode

```
morse encode [-f <file>] [-lang <id>] [-table <file>]
```

```bash
echo "CQ DE W1AW" | morse encode
# -.-. --.- / -.. . / .-- .---- .- .--

echo "SCHÖNE GRÜßE" | morse encode -lang de
```

Words are separated by ` / `. Characters within a word are separated by spaces.

### decode

```
morse decode [-f <file>] [-lang <id>] [-table <file>]
```

```bash
echo "... --- ..." | morse decode
# SOS

echo "---- ---. -. ." | morse decode -lang de
# SCHÖNE  (first word only)
```

Accepts flexible word separators — `/` with any surrounding whitespace
(e.g. `" / "`, `"/"`, `"  /  "`, tabs).

### wav

```
morse wav [-f <file>] [-o <file>] [-mode <mode>] [-lang <id>] [-table <file>]
          [-freq <Hz>] [-dot <ms>] [-amp <0-1>] [-rate <Hz>] [-fade <ms>]
```

| Flag | Default | Description |
|------|---------|-------------|
| `-f` | stdin | Input file |
| `-o` | stdout | Output WAV file |
| `-mode` | `text` | Input mode: `text` (encode first) or `morse` |
| `-freq` | `700` | Tone frequency in Hz |
| `-dot` | `60` | Dot duration in ms (WPM ≈ 1200 ÷ dot) |
| `-amp` | `0.8` | Amplitude, 0.0–1.0 |
| `-rate` | `44100` | PCM sample rate in Hz |
| `-fade` | `5` | Rise/fall cosine fade time in ms |

```bash
echo "SOS" | morse wav -o sos.wav
echo "SOS" | morse wav -freq 800 -dot 80 -amp 0.6 -o sos_slow.wav
echo "... --- ..." | morse wav -mode morse -o sos_from_morse.wav

# Pipe directly to a player
echo "SOS" | morse wav | aplay -
echo "SOS" | morse wav | afplay -
```

**Speed reference:**

| `-dot` (ms) | WPM |
|-------------|-----|
| 120 | 10 |
| 60 | 20 |
| 40 | 30 |
| 24 | 50 |

### langs

```bash
morse langs
```

```
Built-in language tables:

  itu  ITU-R M.1677-1 (Latin + extended European, default)
  en   English – alias for the ITU Latin table
  de   German (DARC) – ITU Latin + Ä Ö Ü ß + SCH trigraph, no CH digraph
  ru   Russian / Cyrillic (DOSAAF) – full Cyrillic alphabet
  zh   Chinese – digit/Latin table for Continental Telegraph Code (CTC) numerics
```

### showtable

Print the full encode table and digraphs for a language.

```bash
morse showtable -lang de
morse showtable -lang ru
morse showtable -lang itu -table my_extras.json
```

---

## Language Support

All subcommands and library functions accept a `-lang`/`langID` value.

### ITU / English (`itu`, `en`)

The default. Covers the full ITU-R M.1677-1 character set: A–Z, 0–9,
punctuation, and extended European accented characters from Annex 1.

```bash
echo "THE QUICK BROWN FOX" | morse encode
echo "73 DE K1ABC" | morse encode
```

```go
ct, _ := morse.ResolveTable("itu", "")
ct.EncodeLine("THE QUICK BROWN FOX")
```

### German (`de`)

Based on the ITU table with DARC/IARU Region 1 conventions:

- **Ä** `.-.-` · **Ö** `---.` · **Ü** `..--` · **ß** `...---.`
- `SCH` is a trigraph encoded as `----`, matched with priority over individual letters via longest-match
- Decoding `----` produces `SCH` (not `Ĥ` as in plain ITU)
- The `CH` digraph is removed — bare `C` and `H` encode as individual letters

```bash
echo "SCHÖNE GRÜßE" | morse encode -lang de
echo "SCHÖNE GRÜßE" | morse encode -lang de | morse decode -lang de
```

```go
ct, _ := morse.ResolveTable("de", "")
code, _ := ct.EncodeLine("SCHÖNE GRÜßE")
text, _ := ct.DecodeLine(code)
// text == "SCHÖNE GRÜßE"
```

### Russian / Cyrillic (`ru`)

Full 33-letter Cyrillic alphabet following the DOSAAF/Soviet standard. Each
Cyrillic letter maps to the same dot-dash pattern as its nearest Latin
equivalent by operator convention, so the rhythm is identical to ITU Morse.

> **Note:** Ё shares the sequence `.` with Е. Ё is accepted on input but
> decodes back as Е — this is standard practice.

```bash
echo "МОСКВА" | morse encode -lang ru
echo "МОСКВА" | morse encode -lang ru | morse decode -lang ru
```

```go
ct, _ := morse.ResolveTable("ru", "")
code, _ := ct.EncodeLine("ПРИВЕТ МИР")
text, _ := ct.DecodeLine(code)
// text == "ПРИВЕТ МИР"
```

### Chinese (`zh`)

Chinese Morse (大陆电码) is a two-stage system:

1. Look up each Hanzi in the [Chinese Telegraph Code (CTC)](https://en.wikipedia.org/wiki/Chinese_telegraph_code) book to get a 4-digit decimal number.
2. Transmit those digits as standard Morse numerals.

The `zh` table covers digits and Latin letters for this purpose. A full
Hanzi↔CTC dictionary is not bundled — use a custom JSON table to add
specific characters you need.

```bash
# 你(4919) 好(1072) — operator looks up CTC codes, then sends digits
echo "4919 1072" | morse encode -lang zh
# ....- ----. .---- ----. / .---- ----- --... ..---
```

```go
ct, _ := morse.ResolveTable("zh", "")
code, _ := ct.EncodeLine("4919 1072")
// code == "....- ----. .---- ----. / .---- ----- --... ..---"
```

---

## Custom Table Format

Any language table can be extended or overridden with a JSON file.

```json
{
  "encode": {
    "#": "...-.-",
    "~": ".-.-."
  },
  "digraphs": {
    "SCH": "----",
    "NG":  "--."
  },
  "priority": ["#", "~", "A", "B"]
}
```

| Field | Description |
|-------|-------------|
| `encode` | Single character → Morse sequence. Keys must be exactly one Unicode character. |
| `digraphs` | Multi-character string → Morse sequence. Matched longest-first during encoding. |
| `priority` | Decode tie-break order. Earlier = higher priority. Replaces the language default when provided. |

The custom file is **merged on top** of the selected language — only the
keys you specify are overridden. The input spec is never mutated; each merge
produces an independent deep copy.

**CLI:**

```bash
echo "73 #" | morse encode -table extras.json
morse encode -lang de -table hanzi.json -f message.txt
```

**Library:**

```go
// Quick path via ResolveTable
ct, err := morse.ResolveTable("itu", "extras.json")

// Full control path
spec, _ := morse.LookupLang("zh")
spec, _ = morse.MergeCustomTable(spec, "hanzi.json")
ct := morse.BuildCodeTable(spec)
```

**Adding Hanzi via CTC lookup:**

```json
{
  "encode": {
    "你": "....-.-.--.----.",
    "好": ".----.--.-.----"
  }
}
```

```go
ct, _ := morse.ResolveTable("zh", "hanzi.json")
code, _ := ct.EncodeLine("你好")
text, _ := ct.DecodeLine(code)
// text == "你好"
```

---

## WAV Audio Details

The WAV generator produces **16-bit mono PCM** files.

### Timing (ITU-R M.1677-1)

| Element | Duration |
|---------|----------|
| Dot | 1 unit |
| Dash | 3 units |
| Intra-character gap | 1 unit |
| Inter-character gap | 3 units |
| Inter-word gap | 7 units |

One unit = `DotMs` milliseconds. Default 60 ms → 20 WPM.

### Fade envelope

A cosine rise/fall envelope (controlled by `FadeMs`) eliminates the audible
key-click caused by hard waveform edges. The fade is automatically clamped
to half the symbol duration when the tone is shorter than `2 × FadeMs`
samples, so very short dots at slow speeds never produce a garbled envelope.
Set `FadeMs: 0` to disable.

### Memory usage

WAV generation is fully streaming: each Morse symbol is rendered into a
small reusable buffer and written incrementally to the encoder. Peak memory
is bounded by the longest single symbol (one dash or one inter-word gap),
not the total recording length — hour-long recordings do not require
hundreds of MB of RAM.

### Seekable vs non-seekable output

The WAV format requires patching chunk-size headers after all samples are
written. When `w` implements `io.WriteSeeker` (e.g. `*os.File`) the encoder
writes directly. For non-seekable writers (pipes, `http.ResponseWriter`,
`bytes.Buffer`) a temporary file is used transparently and streamed to `w`
on completion.

---

## Supported Characters

### ITU-R M.1677-1 — Basic (Table 1)

```
A B C D E F G H I J K L M N O P Q R S T U V W X Y Z
0 1 2 3 4 5 6 7 8 9
. , ? ' ! / ( ) & : ; = + - _ " $ @
```

### ITU-R M.1677-1 — Extended European (Annex 1)

```
À Á Ä Å Æ Ç Ć Ð É È Ê Ĝ Ĥ Ĵ Ł Ñ Ń Ó Ö Ø Ś Š Þ Ü Ŭ Ź Ż
```

**Digraph:** `CH` → `----`

### German additions (DARC)

```
ß
```

**Trigraph:** `SCH` → `----` (replaces `CH` digraph in the `de` table)

### Russian / Cyrillic (DOSAAF)

```
А Б В Г Д Е Ё Ж З И Й К Л М Н О П Р С Т У Ф Х Ц Ч Ш Щ Ъ Ы Ь Э Ю Я
```

### Unicode normalisation

Decomposed Unicode input (NFD) is automatically normalised to NFC before
encoding via a double-pass `NFC → ToUpper → NFC`, so `A` + combining
diaeresis (U+0308) is treated identically to the precomposed `Ä` (U+00C4).
Multiple combining marks and canonical reordering are handled correctly.

---

## Development

```bash
# Run all tests
go test ./...

# Run with race detector
go test -race ./...

# Verbose output
go test -v ./...

# Run a specific test
go test -run TestGermanRoundTrip ./...

# Build the CLI
go build -o morse ./cmd/morse
```
