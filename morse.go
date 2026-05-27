// morse - ITU-R M.1677-1 compliant Morse code encoder / decoder / WAV generator
//
// Usage:
//
//	morse encode [-f file]            Read plain text, write Morse code
//	morse decode [-f file]            Read Morse code, write plain text
//	morse wav    [-f file] [-o file]  Read plain text or Morse, write a WAV audio file
//
// When -f is omitted the tool reads from stdin.
// When -o is omitted for wav, the tool writes to stdout (pipe-friendly).
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"unicode"

	"github.com/go-audio/audio"
	"github.com/go-audio/wav"
)

// ---------------------------------------------------------------------------
// ITU-R M.1677-1 code table  (core + annex / national characters)
// ---------------------------------------------------------------------------

// encodeTable maps Unicode characters → Morse sequences.
//
// Sources:
//   - ITU-R M.1677-1 (2009) Table 1 – Basic characters
//   - ITU-R M.1677-1 Annex 1 – Additional characters for European languages
//   - ARRL/PARIS convention for characters not covered by ITU
var encodeTable = map[rune]string{
	// Basic Latin letters  (ITU Table 1)
	'A': ".-", 'B': "-...", 'C': "-.-.", 'D': "-..",
	'E': ".", 'F': "..-.", 'G': "--.", 'H': "....",
	'I': "..", 'J': ".---", 'K': "-.-", 'L': ".-..",
	'M': "--", 'N': "-.", 'O': "---", 'P': ".--.",
	'Q': "--.-", 'R': ".-.", 'S': "...", 'T': "-",
	'U': "..-", 'V': "...-", 'W': ".--", 'X': "-..-",
	'Y': "-.--", 'Z': "--..",

	// Digits  (ITU Table 1)
	'0': "-----", '1': ".----", '2': "..---", '3': "...--",
	'4': "....-", '5': ".....", '6': "-....", '7': "--...",
	'8': "---..", '9': "----.",

	// Punctuation  (ITU-R M.1677-1 §2 / Table 2)
	'.': ".-.-.-", ',': "--..--", '?': "..--..",
	'\'': ".----.", '!': "-.-.--", '/': "-..-.",
	'(': "-.--.", ')': "-.--.-", '&': ".-...",
	':': "---...", ';': "-.-.-.", '=': "-...-",
	'+': ".-.-.", '-': "-....-", '_': "..--.-",
	'"': ".-..-.", '$': "...-..-", '@': ".--.-.",

	// Accented / national characters  (ITU-R M.1677-1 Annex 1)
	'À': ".--.-", 'Á': ".--.-",
	'Ä': ".-.-",
	'Å': ".--.-",
	'Æ': ".-.-",
	'Ç': "-.-..", 'Ć': "-.-..",
	'Ð': "..--.",
	'É': "..-..",
	'È': ".-..-",
	'Ê': ".-..-",
	'Ĝ': "--.-.",
	'Ĥ': "----",
	'Ĵ': ".---.",
	'Ł': ".-.--",
	'Ń': "--.--", 'Ñ': "--.--",
	'Ó': "---.", 'Ø': "---.",
	'Ö': "---.",
	'Ś': "...-...",
	'Š': "----",
	'Þ': ".--..",
	'Ü': "..--", 'Ŭ': "..--",
	'Ź': "--..-.",
	'Ż': "--..-",
}

// digraphTable maps two-rune sequences that have their own ITU Morse code.
var digraphTable = map[string]string{
	"CH": "----",
}

// decodeTable is the reverse of encodeTable, built at init time.
var decodeTable map[string]rune

// decodePriority defines which rune wins when multiple characters share a
// Morse sequence.  Lower index = higher priority.
var decodePriority = []rune{
	'A', 'B', 'C', 'D', 'E', 'F', 'G', 'H', 'I', 'J', 'K', 'L', 'M',
	'N', 'O', 'P', 'Q', 'R', 'S', 'T', 'U', 'V', 'W', 'X', 'Y', 'Z',
	'0', '1', '2', '3', '4', '5', '6', '7', '8', '9',
	'Á', 'Ä', 'Å', 'Æ', 'Ç', 'Ð', 'É', 'È', 'Ê', 'Ĝ', 'Ĥ', 'Ĵ',
	'Ł', 'Ñ', 'Ó', 'Ö', 'Ø', 'Ś', 'Š', 'Þ', 'Ü', 'Ź', 'Ż',
	'À', 'Ć', 'Ń', 'Ŭ',
}

func init() {
	prio := make(map[rune]int, len(decodePriority))
	for i, r := range decodePriority {
		prio[r] = i
	}
	lowPrio := len(decodePriority)

	decodeTable = make(map[string]rune, len(encodeTable))
	for r, code := range encodeTable {
		existing, collision := decodeTable[code]
		if !collision {
			decodeTable[code] = r
			continue
		}
		ep, ok := prio[existing]
		if !ok {
			ep = lowPrio
		}
		np, ok := prio[r]
		if !ok {
			np = lowPrio
		}
		if np < ep {
			decodeTable[code] = r
		}
	}
}

// combiningToPrecomposed converts a small set of base+combining-diacritic
// pairs that appear in our encode table into their precomposed equivalents.
// This avoids the need for golang.org/x/text just for these characters.
var combiningToPrecomposed = map[string]rune{
	"A\u0300": 'À', "A\u0301": 'Á', "A\u0308": 'Ä', "A\u030A": 'Å',
	"C\u0327": 'Ç', "C\u0301": 'Ć',
	"E\u0301": 'É', "E\u0300": 'È', "E\u0302": 'Ê',
	"G\u0302": 'Ĝ',
	"H\u0302": 'Ĥ',
	"J\u0302": 'Ĵ',
	"L\u0142": 'Ł',
	"N\u0301": 'Ń', "N\u0303": 'Ñ',
	"O\u0301": 'Ó', "O\u0308": 'Ö', "O\u0338": 'Ø',
	"S\u0301": 'Ś', "S\u030C": 'Š',
	"U\u0308": 'Ü', "U\u0306": 'Ŭ',
	"Z\u0301": 'Ź', "Z\u0307": 'Ż',
}

// normAndUpper converts s to upper-case and folds known decomposed forms to
// their precomposed equivalents so the encode table can match them.
func normAndUpper(s string) string {
	s = strings.ToUpper(s)
	runes := []rune(s)
	var out []rune
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if i+1 < len(runes) && unicode.Is(unicode.Mn, runes[i+1]) {
			key := string([]rune{r, runes[i+1]})
			if precomposed, ok := combiningToPrecomposed[key]; ok {
				out = append(out, precomposed)
				i++
				continue
			}
		}
		out = append(out, r)
	}
	return string(out)
}

// encodeLine converts a single line of plain text to Morse code.
func encodeLine(line string) (string, error) {
	var words []string
	for _, word := range strings.Fields(line) {
		upper := normAndUpper(word)
		var tokens []string
		runes := []rune(upper)
		for i := 0; i < len(runes); i++ {
			r := runes[i]
			if unicode.IsSpace(r) {
				continue
			}
			// Check two-rune digraphs first (e.g. "CH")
			if i+1 < len(runes) {
				digraph := string([]rune{r, runes[i+1]})
				if code, ok := digraphTable[digraph]; ok {
					tokens = append(tokens, code)
					i++
					continue
				}
			}
			code, ok := encodeTable[r]
			if !ok {
				return "", fmt.Errorf("unsupported character: %q (U+%04X)", r, r)
			}
			tokens = append(tokens, code)
		}
		if len(tokens) > 0 {
			words = append(words, strings.Join(tokens, " "))
		}
	}
	return strings.Join(words, " / "), nil
}

// encode reads plain text from r and writes Morse code to w.
func encode(r io.Reader, w io.Writer) error {
	scanner := bufio.NewScanner(r)
	bw := bufio.NewWriter(w)
	defer bw.Flush()
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			fmt.Fprintln(bw)
			continue
		}
		encoded, err := encodeLine(line)
		if err != nil {
			return err
		}
		fmt.Fprintln(bw, encoded)
	}
	return scanner.Err()
}

// decodeLine converts a single line of Morse code back to plain text.
func decodeLine(line string) (string, error) {
	var sb strings.Builder
	wordGroups := strings.Split(line, " / ")
	for wi, wg := range wordGroups {
		if wi > 0 {
			sb.WriteRune(' ')
		}
		wg = strings.TrimSpace(wg)
		if wg == "" {
			continue
		}
		for _, token := range strings.Fields(wg) {
			r, ok := decodeTable[token]
			if !ok {
				return "", fmt.Errorf("unknown Morse sequence: %q", token)
			}
			sb.WriteRune(r)
		}
	}
	return sb.String(), nil
}

// decode reads Morse code from r and writes plain text to w.
func decode(r io.Reader, w io.Writer) error {
	scanner := bufio.NewScanner(r)
	bw := bufio.NewWriter(w)
	defer bw.Flush()
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			fmt.Fprintln(bw)
			continue
		}
		decoded, err := decodeLine(line)
		if err != nil {
			return err
		}
		fmt.Fprintln(bw, decoded)
	}
	return scanner.Err()
}

// WavParams holds all audio generation parameters.
type WavParams struct {
	// Tone frequency in Hz (ITU-R recommended sidetone: 700 Hz)
	FreqHz float64

	// Duration of a single dot in milliseconds (60 ms ≈ 20 WPM)
	DotMs int

	// Amplitude 0.0–1.0
	Amplitude float64

	// PCM sample rate in Hz
	SampleRate int

	// Rise/fall cosine-fade time in milliseconds.
	// Eliminates the audible key-click caused by hard waveform edges.
	FadeMs int

	// InputMode: "text" (plain text, encode first) or "morse" (raw dots/dashes)
	InputMode string
}

// symbol is a single timed event on the audio timeline.
type symbol struct {
	on   bool // true = tone, false = silence
	dots int  // duration as a multiple of one dot length
}

// morseLinesToSymbols converts a full Morse string into a flat []symbol slice.
//
// ITU-R M.1677-1 timing ratios (in dot units):
//
//	dot             = 1
//	dash            = 3
//	intra-char gap  = 1   (between elements within one character)
//	inter-char gap  = 3   (between characters within one word)
//	inter-word gap  = 7   (represented by " / " in the text)
func morseLinesToSymbols(morseText string) ([]symbol, error) {
	var syms []symbol

	addSilence := func(dots int) {
		if len(syms) > 0 && !syms[len(syms)-1].on {
			syms[len(syms)-1].dots += dots // merge consecutive silences
		} else {
			syms = append(syms, symbol{on: false, dots: dots})
		}
	}
	addTone := func(dots int) {
		syms = append(syms, symbol{on: true, dots: dots})
	}

	lines := strings.Split(strings.TrimSpace(morseText), "\n")
	firstWord := true
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		wordGroups := strings.Split(line, " / ")
		for wi, wg := range wordGroups {
			wg = strings.TrimSpace(wg)
			if wg == "" {
				continue
			}
			if !firstWord || wi > 0 {
				addSilence(7) // inter-word gap
			}
			firstWord = false

			for ti, tok := range strings.Fields(wg) {
				if ti > 0 {
					addSilence(3) // inter-character gap
				}
				for ei, elem := range tok {
					if ei > 0 {
						addSilence(1) // intra-character gap
					}
					switch elem {
					case '.':
						addTone(1)
					case '-':
						addTone(3)
					default:
						return nil, fmt.Errorf("invalid element %q in token %q", elem, tok)
					}
				}
			}
		}
	}
	return syms, nil
}

// writeWAV renders syms to w as a 16-bit mono PCM WAV file using
// github.com/go-audio/wav.
//
// The encoder requires an io.WriteSeeker so it can patch the RIFF/data chunk
// sizes after writing all samples. When the caller passes an *os.File that is
// fine. For stdout or any non-seekable writer the caller should pass a
// temporary file and copy it afterwards (see generateWAV).
func writeWAV(ws io.WriteSeeker, syms []symbol, p WavParams) error {
	sr := p.SampleRate
	dotSamples := int(math.Round(float64(sr) * float64(p.DotMs) / 1000.0))
	fadeSamples := int(math.Round(float64(sr) * float64(p.FadeMs) / 1000.0))
	paddingSamples := dotSamples / 2
	maxAmp := p.Amplitude * 32767.0 // 16-bit signed full scale

	// --- synthesise PCM samples into a plain []int slice ---
	phase := 0.0
	phaseInc := 2.0 * math.Pi * p.FreqHz / float64(sr)

	var samples []int

	appendSilence := func(n int) {
		for i := 0; i < n; i++ {
			samples = append(samples, 0)
		}
	}

	appendTone := func(n int) {
		for i := 0; i < n; i++ {
			// Cosine-shaped amplitude envelope at leading and trailing edges
			// removes the discontinuity that causes key-click artefacts.
			env := 1.0
			if fadeSamples > 0 {
				switch {
				case i < fadeSamples:
					env = 0.5 * (1.0 - math.Cos(math.Pi*float64(i)/float64(fadeSamples)))
				case i >= n-fadeSamples:
					env = 0.5 * (1.0 - math.Cos(math.Pi*float64(n-1-i)/float64(fadeSamples)))
				}
			}
			samples = append(samples, int(math.Round(maxAmp*env*math.Sin(phase))))
			phase += phaseInc
		}
	}

	appendSilence(paddingSamples)
	for _, s := range syms {
		n := s.dots * dotSamples
		if s.on {
			appendTone(n)
		} else {
			appendSilence(n)
		}
	}
	appendSilence(paddingSamples)

	// --- hand the samples to go-audio/wav ---
	//
	// wav.NewEncoder writes the RIFF/fmt chunk immediately and holds the
	// WriteSeeker open so it can back-patch the data-chunk size on Close().
	enc := wav.NewEncoder(
		ws,
		p.SampleRate,
		16, // bit depth
		1,  // channels (mono)
		1,  // audio format: PCM
	)

	buf := &audio.IntBuffer{
		Data: samples,
		Format: &audio.Format{
			SampleRate:  p.SampleRate,
			NumChannels: 1,
		},
		SourceBitDepth: 16,
	}

	if err := enc.Write(buf); err != nil {
		return fmt.Errorf("wav write: %w", err)
	}
	// Close flushes and patches the RIFF/data chunk size headers.
	if err := enc.Close(); err != nil {
		return fmt.Errorf("wav close: %w", err)
	}
	return nil
}

// generateWAV is the top-level entry point for the wav subcommand.
//
// go-audio/wav's encoder requires an io.WriteSeeker so it can patch chunk
// sizes after writing all samples. Regular files satisfy this. When the
// destination is stdout (non-seekable) we write to a temp file first and then
// stream it to stdout.
func generateWAV(r io.Reader, w io.Writer, p WavParams) error {
	// Collect and optionally encode the input text.
	var sb strings.Builder
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		sb.WriteString(scanner.Text())
		sb.WriteRune('\n')
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	input := strings.TrimSpace(sb.String())
	if input == "" {
		return fmt.Errorf("empty input")
	}

	var morseText string
	if p.InputMode == "text" {
		var morseLines []string
		for _, line := range strings.Split(input, "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			encoded, err := encodeLine(line)
			if err != nil {
				return fmt.Errorf("encoding: %w", err)
			}
			morseLines = append(morseLines, encoded)
		}
		morseText = strings.Join(morseLines, "\n")
	} else {
		morseText = input
	}

	syms, err := morseLinesToSymbols(morseText)
	if err != nil {
		return fmt.Errorf("parsing morse: %w", err)
	}
	if len(syms) == 0 {
		return fmt.Errorf("no symbols to render")
	}

	// If w is already an io.WriteSeeker (i.e. a real file) use it directly.
	if ws, ok := w.(io.WriteSeeker); ok {
		return writeWAV(ws, syms, p)
	}

	// Otherwise (stdout, pipe, …) write to a temp file then copy to w.
	tmp, err := os.CreateTemp("", "morse-wav-*.wav")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	if err := writeWAV(tmp, syms, p); err != nil {
		return err
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("seeking temp file: %w", err)
	}
	_, err = io.Copy(w, tmp)
	return err
}

func usage() {
	fmt.Fprintln(os.Stderr, `morse - ITU-R M.1677-1 Morse code encoder / decoder / WAV generator

USAGE
  morse encode [-f <file>]   Encode plain text → Morse code
  morse decode [-f <file>]   Decode Morse code → plain text
  morse wav    [flags]       Render plain text or Morse code → WAV audio

ENCODE / DECODE FLAGS
  -f <file>   Input file (default: stdin)

WAV FLAGS
  -f <file>       Input file (default: stdin)
  -o <file>       Output WAV file (default: stdout)
  -mode <mode>    Input mode: "text" (default) or "morse"
                    text  – plain text is encoded to Morse automatically
                    morse – input is already in Morse format (".-" etc.)
  -freq <Hz>      Tone frequency in Hz        (default: 700)
  -dot  <ms>      Dot duration in ms          (default: 60  ≈ 20 WPM)
  -amp  <0-1>     Amplitude / loudness 0–1    (default: 0.8)
  -rate <Hz>      Sample rate in Hz           (default: 44100)
  -fade <ms>      Rise/fall fade time in ms   (default: 5, set 0 to disable)

WAV TIMING (ITU-R M.1677-1 ratios, in dot units)
  Dash = 3  ·  intra-char gap = 1  ·  inter-char gap = 3  ·  word gap = 7
  Speed in WPM ≈ 1200 ÷ dot_ms  (default 60 ms → ~20 WPM)

SUPPORTED EXTENDED CHARACTERS (ITU-R M.1677-1 Annex 1)
  À Á Ä Å Æ Ç Ć Ð É È Ê Ĝ Ĥ Ĵ Ł Ñ Ń Ó Ö Ø Ś Š Þ Ü Ŭ Ź Ż
  Digraph: CH (encoded as ----)

EXAMPLES
  echo "HELLO WORLD"    | morse encode
  echo "Ä Ö Ü"         | morse encode
  echo "SOS"            | morse wav -o sos.wav
  echo "SOS"            | morse wav -freq 800 -dot 80 -amp 0.6 -o sos_slow.wav
  echo "... --- ..."    | morse wav -mode morse -o sos_morse.wav
  morse wav -f msg.txt -o msg.wav -freq 600 -dot 50 -amp 1.0 -rate 22050`)
}

func main() {
	encodeCmd := flag.NewFlagSet("encode", flag.ExitOnError)
	encodeFile := encodeCmd.String("f", "", "input file (default: stdin)")

	decodeCmd := flag.NewFlagSet("decode", flag.ExitOnError)
	decodeFile := decodeCmd.String("f", "", "input file (default: stdin)")

	wavCmd := flag.NewFlagSet("wav", flag.ExitOnError)
	wavInFile := wavCmd.String("f", "", "input file (default: stdin)")
	wavOutFile := wavCmd.String("o", "", "output WAV file (default: stdout)")
	wavMode := wavCmd.String("mode", "text", `input mode: "text" or "morse"`)
	wavFreq := wavCmd.Float64("freq", 700, "tone frequency in Hz")
	wavDot := wavCmd.Int("dot", 60, "dot duration in milliseconds (WPM ≈ 1200/dot)")
	wavAmp := wavCmd.Float64("amp", 0.8, "amplitude 0.0–1.0")
	wavRate := wavCmd.Int("rate", 44100, "sample rate in Hz")
	wavFade := wavCmd.Int("fade", 5, "rise/fall fade time in ms")

	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	openInput := func(path string) (io.ReadCloser, error) {
		if path == "" {
			return io.NopCloser(os.Stdin), nil
		}
		return os.Open(path)
	}

	switch os.Args[1] {
	case "encode":
		encodeCmd.Parse(os.Args[2:])
		in, err := openInput(*encodeFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "morse: %v\n", err)
			os.Exit(1)
		}
		defer in.Close()
		if err := encode(in, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "morse encode: %v\n", err)
			os.Exit(1)
		}

	case "decode":
		decodeCmd.Parse(os.Args[2:])
		in, err := openInput(*decodeFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "morse: %v\n", err)
			os.Exit(1)
		}
		defer in.Close()
		if err := decode(in, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "morse decode: %v\n", err)
			os.Exit(1)
		}

	case "wav":
		wavCmd.Parse(os.Args[2:])

		if *wavAmp < 0 || *wavAmp > 1 {
			fmt.Fprintln(os.Stderr, "morse wav: -amp must be between 0.0 and 1.0")
			os.Exit(1)
		}
		if *wavFreq <= 0 {
			fmt.Fprintln(os.Stderr, "morse wav: -freq must be > 0")
			os.Exit(1)
		}
		if *wavDot <= 0 {
			fmt.Fprintln(os.Stderr, "morse wav: -dot must be > 0")
			os.Exit(1)
		}
		if *wavRate < 8000 {
			fmt.Fprintln(os.Stderr, "morse wav: -rate must be >= 8000")
			os.Exit(1)
		}
		if *wavFade < 0 {
			fmt.Fprintln(os.Stderr, "morse wav: -fade must be >= 0")
			os.Exit(1)
		}
		if *wavMode != "text" && *wavMode != "morse" {
			fmt.Fprintln(os.Stderr, `morse wav: -mode must be "text" or "morse"`)
			os.Exit(1)
		}

		in, err := openInput(*wavInFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "morse: %v\n", err)
			os.Exit(1)
		}
		defer in.Close()

		p := WavParams{
			FreqHz:     *wavFreq,
			DotMs:      *wavDot,
			Amplitude:  *wavAmp,
			SampleRate: *wavRate,
			FadeMs:     *wavFade,
			InputMode:  *wavMode,
		}

		var out io.Writer
		if *wavOutFile == "" {
			out = os.Stdout
		} else {
			f, err := os.Create(*wavOutFile)
			if err != nil {
				fmt.Fprintf(os.Stderr, "morse wav: %v\n", err)
				os.Exit(1)
			}
			defer f.Close()
			out = f
		}

		if err := generateWAV(in, out, p); err != nil {
			fmt.Fprintf(os.Stderr, "morse wav: %v\n", err)
			os.Exit(1)
		}

		if *wavOutFile != "" {
			wpm := 1200 / *wavDot
			fmt.Fprintf(os.Stderr,
				"morse wav: wrote %s  [%g Hz · %d ms dot · ~%d WPM · amp %.2f · %d Hz sample rate]\n",
				*wavOutFile, *wavFreq, *wavDot, wpm, *wavAmp, *wavRate)
		}

	case "-h", "--help", "help":
		usage()

	default:
		fmt.Fprintf(os.Stderr, "morse: unknown subcommand %q\n\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}
