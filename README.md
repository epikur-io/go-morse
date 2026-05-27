# Morse

**ITU-R M.1677-1 compliant Morse code encoder / decoder / WAV generator**

A command-line tool written in Go for encoding and decoding Morse code, with full support for WAV audio synthesis, extended character sets, and proper ITU timing rules.

## Features

- Encode plain text → Morse code
- Decode Morse code → plain text
- Generate WAV audio from text or Morse input
- ITU-R M.1677-1 compliant encoding rules
- Extended accented character support (Annex 1)
- Digraph support (`CH → ----`)
- Pipe-friendly CLI design
- Smooth cosine fade-in/out audio synthesis
- Configurable tone frequency, speed, and sample rate

## Installation

### Build from source

```bash
git clone https://github.com/epikur-io/go-morse.git
cd morse
go build -o morse
````

### Dependencies

```bash
go get github.com/go-audio/audio
go get github.com/go-audio/wav
```


## Usage

### General syntax

```bash
morse encode [-f file]
morse decode [-f file]
morse wav    [-f file] [-o file] [options]
```

* If `-f` is omitted → reads from **stdin**
* If `-o` is omitted (wav) → writes to **stdout**

## Commands

## Encode (text → Morse)

```bash
echo "HELLO WORLD" | morse encode
```

Output:

```
.... . .-.. .-.. --- / .-- --- .-. .-.. -..
```

### File input

```bash
morse encode -f input.txt
```

## Decode (Morse → text)

```bash
echo ".... . .-.. .-.. ---" | morse decode
```

Output:

```
HELLO
```

### Word separator

* `/` = word boundary
* space = character separator

## WAV generation (text or Morse → audio)

### From text

```bash
echo "SOS" | morse wav -o sos.wav
```

### From Morse input

```bash
echo "... --- ..." | morse wav -mode morse -o sos.wav
```

### Pipe output

```bash
echo "HELLO WORLD" | morse wav > output.wav
```

## WAV Options

| Flag    | Description         | Default |
| ------- | ------------------- | ------- |
| `-freq` | Tone frequency (Hz) | 700     |
| `-dot`  | Dot duration (ms)   | 60      |
| `-amp`  | Amplitude (0.0–1.0) | 0.8     |
| `-rate` | Sample rate (Hz)    | 44100   |
| `-fade` | Fade-in/out (ms)    | 5       |
| `-mode` | `text` or `morse`   | text    |

### Example: slow Morse

```bash
echo "TEST" | morse wav -dot 120 -freq 600
```

## Timing Rules (ITU-R M.1677-1)

| Element             | Duration |
| ------------------- | -------- |
| Dot                 | 1 unit   |
| Dash                | 3 units  |
| Intra-character gap | 1 unit   |
| Inter-character gap | 3 units  |
| Inter-word gap      | 7 units  |

### Speed estimate

```
WPM ≈ 1200 / dot_ms
```

Example:

* 60 ms dot → ~20 WPM

## Supported Characters

### Letters

A–Z

### Numbers

0–9

### Punctuation

```
. , ? ' ! / ( ) & : ; = + - _ " $ @
```

### Extended characters (ITU Annex 1)

```
À Á Ä Å Æ Ç Ć Ð É È Ê Ĝ Ĥ Ĵ Ł
Ñ Ń Ó Ö Ø Ś Š Þ Ü Ŭ Ź Ż
```

### Digraphs

```
CH → ----
```

## Architecture Overview

### Encoding

* Unicode normalization (combining → precomposed)
* Character → Morse mapping via lookup table
* Word separation using `/`

Key function:

```go
encodeLine()
```

### Decoding

* Morse sequences mapped back to runes
* Collision resolution via priority table

Key function:

```go
decodeLine()
```

### WAV generation

Pipeline:

```
Text/Morse → Symbols → PCM Samples → WAV File
```

Key components:

* `morseLinesToSymbols()` → timing model
* `writeWAV()` → audio synthesis
* cosine fade envelope → click-free output
* 16-bit PCM WAV encoding

## Audio Design

* Default tone: 700 Hz (ITU sidetone standard)
* Cosine fade-in/out to remove key clicks
* Continuous-phase sine wave (no discontinuities)
* Mono 16-bit PCM output

## Input Modes

### Text mode (default)

```bash
morse wav -mode text
```

* Input is plain text
* Automatically encoded to Morse

### Morse mode

```bash
morse wav -mode morse
```

* Input already contains `. - /` Morse data
* Skips encoding step

## Examples

### Basic encoding

```bash
echo "HELLO WORLD" | morse encode
```

### High-frequency tone WAV

```bash
echo "SOS" | morse wav -freq 900 -amp 0.6 -o sos.wav
```

### Slow training audio

```bash
echo "CQ CQ CQ" | morse wav -dot 150 -freq 600
```

### Decode file

```bash
morse decode -f message.morse
```

## Error Handling

Common errors:

* Unsupported character
* Unknown Morse sequence
* Invalid symbol in Morse input
* Empty input

Example:

```
morse encode: unsupported character '€'
```

## Limitations

* Partial Unicode normalization (no full ICU support)
* Limited digraph support (only `CH`)
* No Farnsworth timing mode
* No real-time keyer input

---

## Dependencies

* Go standard library
* `github.com/go-audio/audio`
* `github.com/go-audio/wav`

