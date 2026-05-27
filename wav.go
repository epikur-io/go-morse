package morse

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strings"

	"github.com/go-audio/audio"
	"github.com/go-audio/wav"
)

type WavParams struct {
	FreqHz     float64
	DotMs      int
	Amplitude  float64
	SampleRate int
	FadeMs     int
	InputMode  string
}

func ValidateWavParams(p WavParams) error {
	var errs []string
	if p.Amplitude < 0 || p.Amplitude > 1 {
		errs = append(errs, "-amp must be between 0.0 and 1.0")
	}
	if p.FreqHz <= 0 {
		errs = append(errs, "-freq must be > 0")
	}
	if p.DotMs <= 0 {
		errs = append(errs, "-dot must be > 0")
	}
	if p.SampleRate < 8000 {
		errs = append(errs, "-rate must be >= 8000")
	}
	if p.FadeMs < 0 {
		errs = append(errs, "-fade must be >= 0")
	}
	if p.InputMode != "text" && p.InputMode != "morse" {
		errs = append(errs, `-mode must be "text" or "morse"`)
	}
	dotSamples64 := int64(p.SampleRate) * int64(p.DotMs) / 1000
	if dotSamples64 > int64(math.MaxInt) {
		errs = append(errs, fmt.Sprintf(
			"-rate %d × -dot %d ms overflows int (%d samples); reduce one or both",
			p.SampleRate, p.DotMs, dotSamples64))
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

type symbol struct {
	on   bool
	dots int
}

func morseLinesToSymbols(morseText string) ([]symbol, error) {
	var syms []symbol
	addSilence := func(dots int) {
		if len(syms) > 0 && !syms[len(syms)-1].on {
			syms[len(syms)-1].dots += dots
		} else {
			syms = append(syms, symbol{false, dots})
		}
	}
	addTone := func(dots int) { syms = append(syms, symbol{true, dots}) }

	firstWord := true
	for _, line := range strings.Split(strings.TrimSpace(morseText), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		for wi, seg := range strings.Split(line, "/") {
			seg = strings.TrimSpace(seg)
			if seg == "" {
				continue
			}
			if !firstWord || wi > 0 {
				addSilence(unitInterWord)
			}
			firstWord = false
			for ti, tok := range strings.Fields(seg) {
				if ti > 0 {
					addSilence(unitInterChar)
				}
				for ei, elem := range tok {
					if ei > 0 {
						addSilence(unitIntraChar)
					}
					switch elem {
					case '.':
						addTone(unitDot)
					case '-':
						addTone(unitDash)
					default:
						return nil, fmt.Errorf("invalid element %q in token %q", elem, tok)
					}
				}
			}
		}
	}
	return syms, nil
}

// writeWAV renders syms as a 16-bit mono PCM WAV file using incremental
// chunk writes.  Each symbol (tone or silence) is rendered into a small
// reusable buffer and streamed to the encoder, so peak memory is bounded by
// the longest single symbol rather than the total recording length.
func writeWAV(ws io.WriteSeeker, syms []symbol, p WavParams) error {
	sr := p.SampleRate
	dotSamples := int(int64(sr) * int64(p.DotMs) / 1000)
	fadeSamples := int(int64(sr) * int64(p.FadeMs) / 1000)
	maxAmp := p.Amplitude * 32767.0
	twoPi := 2.0 * math.Pi
	phaseInc := twoPi * p.FreqHz / float64(sr)
	phase := 0.0

	enc := wav.NewEncoder(ws, p.SampleRate, 16, 1, 1)

	// writeChunk ships n samples to the encoder, reusing buf across calls.
	// The phase accumulator is updated in place so waveform continuity is
	// maintained across chunk boundaries.
	writeChunk := func(buf []int, n int, tone bool) error {
		buf = buf[:n]
		if !tone {
			for i := range buf {
				buf[i] = 0
			}
		} else {
			fade := fadeSamples
			if fade*2 > n {
				fade = n / 2
			}
			for i := range buf {
				env := 1.0
				if fade > 0 {
					switch {
					case i < fade:
						env = 0.5 * (1.0 - math.Cos(math.Pi*float64(i)/float64(fade)))
					case i >= n-fade:
						env = 0.5 * (1.0 - math.Cos(math.Pi*float64(n-1-i)/float64(fade)))
					}
				}
				buf[i] = int(math.Round(maxAmp * env * math.Sin(phase)))
				phase = math.Mod(phase+phaseInc, twoPi)
			}
		}
		return enc.Write(&audio.IntBuffer{
			Data:           buf,
			Format:         &audio.Format{SampleRate: p.SampleRate, NumChannels: 1},
			SourceBitDepth: 16,
		})
	}

	// maxSymbolSamples is the largest single-symbol allocation we will ever
	// need: a dash (3 dot-lengths) is the longest tone; an inter-word gap
	// (7 dot-lengths) is the longest silence.
	maxSymbolSamples := unitInterWord * dotSamples
	// Add the leading/trailing padding (half a dot each) to the cap so the
	// padding writes never need to grow the buffer.
	if dotSamples/2 > maxSymbolSamples {
		maxSymbolSamples = dotSamples / 2
	}
	buf := make([]int, maxSymbolSamples)

	// Leading silence padding.
	if err := writeChunk(buf, dotSamples/2, false); err != nil {
		return fmt.Errorf("wav write: %w", err)
	}

	for _, s := range syms {
		n := s.dots * dotSamples
		if err := writeChunk(buf, n, s.on); err != nil {
			return fmt.Errorf("wav write: %w", err)
		}
	}

	// Trailing silence padding.
	if err := writeChunk(buf, dotSamples/2, false); err != nil {
		return fmt.Errorf("wav write: %w", err)
	}

	if err := enc.Close(); err != nil {
		return fmt.Errorf("wav close: %w", err)
	}
	return nil
}

// GenerateWAV is the top-level WAV entry point.
// The named return allows the deferred cleanup to capture and surface a
// Close error that would otherwise be silently discarded.
func GenerateWAV(r io.Reader, w io.Writer, p WavParams, ct *CodeTable) (err error) {
	var sb strings.Builder
	scanner := newScanner(r)
	for scanner.Scan() {
		sb.WriteString(scanner.Text())
		sb.WriteRune('\n')
	}
	if err = scanner.Err(); err != nil {
		return
	}
	input := strings.TrimSpace(sb.String())
	if input == "" {
		return fmt.Errorf("empty input")
	}

	var morseText string
	if p.InputMode == "text" {
		var lines []string
		for _, line := range strings.Split(input, "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var enc string
			enc, err = ct.EncodeLine(line)
			if err != nil {
				return fmt.Errorf("encoding: %w", err)
			}
			lines = append(lines, enc)
		}
		morseText = strings.Join(lines, "\n")
	} else {
		morseText = input
	}

	syms, symErr := morseLinesToSymbols(morseText)
	if symErr != nil {
		return fmt.Errorf("parsing morse: %w", symErr)
	}
	if len(syms) == 0 {
		return fmt.Errorf("no symbols to render")
	}

	if ws, ok := w.(io.WriteSeeker); ok {
		return writeWAV(ws, syms, p)
	}

	tmp, tmpErr := os.CreateTemp("", "morse-wav-*.wav")
	if tmpErr != nil {
		return fmt.Errorf("creating temp file: %w", tmpErr)
	}
	tmpName := tmp.Name()
	// Deferred cleanup: always remove the temp file, and surface any Close
	// error when no earlier error has already been recorded.
	defer func() {
		if cerr := tmp.Close(); err == nil && cerr != nil {
			err = fmt.Errorf("closing temp file: %w", cerr)
		}
		os.Remove(tmpName)
	}()

	if err = writeWAV(tmp, syms, p); err != nil {
		return
	}
	if _, err = tmp.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("seeking temp file: %w", err)
	}
	if _, err = io.Copy(w, tmp); err != nil {
		return fmt.Errorf("copying wav to output: %w", err)
	}
	return
}
