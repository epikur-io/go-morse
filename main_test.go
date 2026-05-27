package main

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// encodeLine tests
// ---------------------------------------------------------------------------

var encodeTests = []struct {
	name  string
	input string
	want  string
}{
	{"single letter E", "E", "."},
	{"single letter S", "S", "..."},
	{"SOS", "SOS", "... --- ..."},
	{"lowercase normalised", "sos", "... --- ..."},
	{"two words", "HELLO WORLD", ".... . .-.. .-.. --- / .-- --- .-. .-.. -.."},
	{"digit", "5", "....."},
	{"mixed alphanumeric", "CQ3", "-.-. --.- ...--"},
	{"punctuation period", ".", ".-.-.-"},
	{"empty string", "", ""},
}

func TestEncodeLine(t *testing.T) {
	for _, tc := range encodeTests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := encodeLine(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("encodeLine(%q)\n got  %q\n want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestEncodeLineUnsupportedChar(t *testing.T) {
	_, err := encodeLine("µ")
	if err == nil {
		t.Fatal("expected error for unsupported rune, got nil")
	}
}

// ---------------------------------------------------------------------------
// decodeLine tests
// ---------------------------------------------------------------------------

var decodeTests = []struct {
	name  string
	input string
	want  string
}{
	{"single dot", ".", "E"},
	{"SOS", "... --- ...", "SOS"},
	{"two words", ".... . .-.. .-.. --- / .-- --- .-. .-.. -..", "HELLO WORLD"},
	{"digit 0", "-----", "0"},
	{"mixed", "-.-. --.- ...--", "CQ3"},
	{"period", ".-.-.-", "."},
}

func TestDecodeLine(t *testing.T) {
	for _, tc := range decodeTests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decodeLine(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("decodeLine(%q)\n got  %q\n want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestDecodeLineUnknownSequence(t *testing.T) {
	_, err := decodeLine(".....-")
	if err == nil {
		t.Fatal("expected error for unknown sequence, got nil")
	}
}

// ---------------------------------------------------------------------------
// Round-trip tests
// ---------------------------------------------------------------------------

var roundTripTexts = []string{
	"HELLO WORLD",
	"SOS",
	"CQ DE W1AW",
	"THE QUICK BROWN FOX",
	"73 DE K1ABC",
	"PACK MY BOX WITH FIVE DOZEN LIQUOR JUGS",
}

func TestRoundTrip(t *testing.T) {
	for _, original := range roundTripTexts {
		t.Run(original, func(t *testing.T) {
			// encode
			encoded, err := encodeLine(original)
			if err != nil {
				t.Fatalf("encode error: %v", err)
			}
			// decode
			decoded, err := decodeLine(encoded)
			if err != nil {
				t.Fatalf("decode error: %v", err)
			}
			if decoded != original {
				t.Errorf("round-trip failed:\n original %q\n encoded  %q\n decoded  %q",
					original, encoded, decoded)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Stream (encode / decode) tests
// ---------------------------------------------------------------------------

func TestEncodeStream(t *testing.T) {
	in := strings.NewReader("SOS\nHELLO WORLD\n")
	var out strings.Builder
	if err := encode(in, &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 output lines, got %d", len(lines))
	}
	if lines[0] != "... --- ..." {
		t.Errorf("line 1: got %q, want %q", lines[0], "... --- ...")
	}
	if lines[1] != ".... . .-.. .-.. --- / .-- --- .-. .-.. -.." {
		t.Errorf("line 2: got %q", lines[1])
	}
}

func TestDecodeStream(t *testing.T) {
	morse := "... --- ...\n.... . .-.. .-.. --- / .-- --- .-. .-.. -..\n"
	in := strings.NewReader(morse)
	var out strings.Builder
	if err := decode(in, &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if lines[0] != "SOS" {
		t.Errorf("line 1: got %q, want SOS", lines[0])
	}
	if lines[1] != "HELLO WORLD" {
		t.Errorf("line 2: got %q, want HELLO WORLD", lines[1])
	}
}
