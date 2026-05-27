package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	. "github.com/epikur-io/go-morse"
)

func usage() {
	fmt.Fprintln(os.Stderr, `morse - ITU-R M.1677-1 Morse code encoder / decoder / WAV generator

USAGE
  morse encode  [-f <file>] [-lang <id>] [-table <file>]
  morse decode  [-f <file>] [-lang <id>] [-table <file>]
  morse wav     [audio flags] [-lang <id>] [-table <file>]
  morse langs                List built-in language tables
  morse showtable [-lang <id>] [-table <file>]

LANGUAGE FLAGS
  -lang  <id>     Table to use (default: itu).  Run "morse langs" for all IDs.
  -table <file>   JSON file merged on top of -lang.  Schema:
                    { "encode": {"A":".-"}, "digraphs": {"SCH":"----"},
                      "priority": ["A","B"] }

ENCODE / DECODE FLAGS
  -f <file>   Input file (default: stdin)

WAV FLAGS
  -f <file>       Input file (default: stdin)
  -o <file>       Output WAV file (default: stdout)
  -mode <mode>    "text" (default) or "morse"
  -freq <Hz>      Tone frequency in Hz     (default: 700)
  -dot  <ms>      Dot duration in ms       (default: 60 ≈ 20 WPM)
  -amp  <0-1>     Amplitude 0–1            (default: 0.8)
  -rate <Hz>      Sample rate in Hz        (default: 44100)
  -fade <ms>      Rise/fall fade ms        (default: 5)

EXAMPLES
  echo "HELLO WORLD"     | morse encode
  echo "Привет мир"      | morse encode -lang ru
  echo "SCHÖNE GRÜßE"    | morse encode -lang de
  echo "1234"            | morse encode -lang zh
  echo "SOS"             | morse wav -o sos.wav
  morse showtable -lang de
  morse langs`)
}

func main() {
	encodeCmd := flag.NewFlagSet("encode", flag.ExitOnError)
	encodeFile := encodeCmd.String("f", "", "input file (default: stdin)")
	encodeLang, encodeTableFile := AddLangFlags(encodeCmd)

	decodeCmd := flag.NewFlagSet("decode", flag.ExitOnError)
	decodeFile := decodeCmd.String("f", "", "input file (default: stdin)")
	decodeLang, decodeTableFile := AddLangFlags(decodeCmd)

	wavCmd := flag.NewFlagSet("wav", flag.ExitOnError)
	wavInFile := wavCmd.String("f", "", "input file (default: stdin)")
	wavOutFile := wavCmd.String("o", "", "output WAV file (default: stdout)")
	wavMode := wavCmd.String("mode", "text", `input mode: "text" or "morse"`)
	wavFreq := wavCmd.Float64("freq", 700, "tone frequency in Hz")
	wavDot := wavCmd.Int("dot", 60, "dot duration in milliseconds (WPM ≈ 1200/dot)")
	wavAmp := wavCmd.Float64("amp", 0.8, "amplitude 0.0–1.0")
	wavRate := wavCmd.Int("rate", 44100, "sample rate in Hz")
	wavFade := wavCmd.Int("fade", 5, "rise/fall fade time in ms")
	wavLang, wavTableFile := AddLangFlags(wavCmd)

	showTableCmd := flag.NewFlagSet("showtable", flag.ExitOnError)
	showTableLang, showTableFile := AddLangFlags(showTableCmd)

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
		ct, err := ResolveTable(*encodeLang, *encodeTableFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "morse encode: %v\n", err)
			os.Exit(1)
		}
		in, err := openInput(*encodeFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "morse: %v\n", err)
			os.Exit(1)
		}
		defer in.Close()
		if err := ct.Encode(in, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "morse encode: %v\n", err)
			os.Exit(1)
		}

	case "decode":
		decodeCmd.Parse(os.Args[2:])
		ct, err := ResolveTable(*decodeLang, *decodeTableFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "morse decode: %v\n", err)
			os.Exit(1)
		}
		in, err := openInput(*decodeFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "morse: %v\n", err)
			os.Exit(1)
		}
		defer in.Close()
		if err := ct.Decode(in, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "morse decode: %v\n", err)
			os.Exit(1)
		}

	case "wav":
		wavCmd.Parse(os.Args[2:])
		p := WavParams{
			FreqHz:     *wavFreq,
			DotMs:      *wavDot,
			Amplitude:  *wavAmp,
			SampleRate: *wavRate,
			FadeMs:     *wavFade,
			InputMode:  *wavMode,
		}
		if err := ValidateWavParams(p); err != nil {
			fmt.Fprintf(os.Stderr, "morse wav: %v\n", err)
			os.Exit(1)
		}
		ct, err := ResolveTable(*wavLang, *wavTableFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "morse wav: %v\n", err)
			os.Exit(1)
		}
		in, err := openInput(*wavInFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "morse: %v\n", err)
			os.Exit(1)
		}
		defer in.Close()

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
		if err := GenerateWAV(in, out, p, ct); err != nil {
			fmt.Fprintf(os.Stderr, "morse wav: %v\n", err)
			os.Exit(1)
		}
		if *wavOutFile != "" {
			fmt.Fprintf(os.Stderr,
				"morse wav: wrote %s  [%g Hz · %d ms dot · ~%d WPM · amp %.2f · %d Hz sample rate]\n",
				*wavOutFile, *wavFreq, *wavDot, 1200 / *wavDot, *wavAmp, *wavRate)
		}

	case "langs":
		PrintLangs(os.Stdout)

	case "showtable":
		showTableCmd.Parse(os.Args[2:])
		ct, err := ResolveTable(*showTableLang, *showTableFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "morse showtable: %v\n", err)
			os.Exit(1)
		}
		label := *showTableLang
		if *showTableFile != "" {
			label += "+" + *showTableFile
		}
		PrintTableContents(os.Stdout, ct, label)

	case "-h", "--help", "help":
		usage()

	default:
		fmt.Fprintf(os.Stderr, "morse: unknown subcommand %q\n\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}
