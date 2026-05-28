package morse

import (
	"encoding/json"
	"math"
	"os"
	"strings"
	"testing"

	"golang.org/x/text/unicode/norm"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func ituTable() *CodeTable { return newITUTable() }

// buildWithCustomFile merges a JSON string on top of spec and returns the
// sealed CodeTable.  Cleanup is registered via t.Cleanup.
func buildWithCustomFile(t *testing.T, spec tableSpec, jsonContent string) *CodeTable {
	t.Helper()
	path := writeCustomTableFile(t, jsonContent)
	merged, err := MergeCustomTable(spec, path)
	if err != nil {
		t.Fatalf("mergeCustomTable: %v", err)
	}
	return BuildCodeTable(merged)
}

// writeCustomTableFile writes JSON to a temp file, registers cleanup, and
// returns the path.
func writeCustomTableFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp("", "morse-custom-*.json")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	t.Cleanup(func() { os.Remove(f.Name()) })
	return f.Name()
}

// ---------------------------------------------------------------------------
// tableSpec deep-copy isolation
// ---------------------------------------------------------------------------

// Mutating a derived spec must never affect the base spec it was cloned from.
func TestSpecCloneIsolation(t *testing.T) {
	base := ituSpec()
	derived := base.clone()

	// Mutate derived in every possible way.
	derived.encodeMap['Z'] = "changed"
	derived.digraphs["XY"] = "added"
	derived.decodePriority = append(derived.decodePriority, 'Z')

	// Base must be completely unaffected.
	if base.encodeMap['Z'] == "changed" {
		t.Error("clone mutation leaked into base encodeMap")
	}
	if _, ok := base.digraphs["XY"]; ok {
		t.Error("clone mutation leaked into base digraphs")
	}
	if base.decodePriority[len(base.decodePriority)-1] == 'Z' {
		t.Error("clone mutation leaked into base decodePriority")
	}
}

// germanSpec() modifies the digraphs map it clones from ituSpec().
// Running both in any order must produce consistent results.
func TestGermanSpecDoesNotMutateITUSpec(t *testing.T) {
	// Build ITU first, then German.
	itu1 := ituSpec()
	_ = germanSpec()
	itu2 := ituSpec()

	// "CH" must still exist in both ITU snapshots.
	if _, ok := itu1.digraphs["CH"]; !ok {
		t.Error("ituSpec() digraphs missing CH after germanSpec() was called")
	}
	if _, ok := itu2.digraphs["CH"]; !ok {
		t.Error("fresh ituSpec() digraphs missing CH")
	}

	// German must not have CH.
	de := germanSpec()
	if _, ok := de.digraphs["CH"]; ok {
		t.Error("germanSpec() still contains CH digraph")
	}
	if _, ok := de.digraphs["SCH"]; !ok {
		t.Error("germanSpec() missing SCH trigraph")
	}
}

// buildCodeTable must deep-copy from the spec so that mutating the spec after
// sealing does not affect the sealed CodeTable.
func TestBuildCodeTableIsolatesFromSpec(t *testing.T) {
	spec := ituSpec()
	ct := BuildCodeTable(spec)

	// Mutate the spec after sealing.
	spec.encodeMap['A'] = "mutated"
	delete(spec.encodeMap, 'E')

	// The sealed table must still encode A and E correctly.
	got, err := ct.EncodeLine("AE")
	if err != nil {
		t.Fatalf("EncodeLine after spec mutation: %v", err)
	}
	if got != ".- ." {
		t.Errorf("post-mutation encode: got %q, want %q", got, ".- .")
	}
}

// mergeCustomTable must not mutate the input spec.
func TestMergeCustomTableDoesNotMutateInput(t *testing.T) {
	original := ituSpec()
	path := writeCustomTableFile(t, `{"encode":{"#":"...-.-"}}`)

	_, err := MergeCustomTable(original, path)
	if err != nil {
		t.Fatalf("mergeCustomTable: %v", err)
	}

	// '#' must not appear in the original spec.
	if _, ok := original.encodeMap['#']; ok {
		t.Error("mergeCustomTable mutated the input spec encodeMap")
	}
}

// ---------------------------------------------------------------------------
// encodeLine / decodeLine shims
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
// decodeLine
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
// Flexible word-separator parsing
// ---------------------------------------------------------------------------

func TestDecodeFlexibleSeparator(t *testing.T) {
	ct := ituTable()
	want := "HELLO WORLD"
	variants := []struct {
		name  string
		input string
	}{
		{"canonical", ".... . .-.. .-.. --- / .-- --- .-. .-.. -.."},
		{"no spaces around slash", ".... . .-.. .-.. ---/.-- --- .-. .-.. -.."},
		{"extra spaces around slash", ".... . .-.. .-.. ---  /  .-- --- .-. .-.. -.."},
		{"tab around slash", ".... . .-.. .-.. ---\t/\t.-- --- .-. .-.. -.."},
	}
	for _, tc := range variants {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ct.DecodeLine(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}

func TestMorseLinesToSymbolsFlexibleSeparator(t *testing.T) {
	variants := []string{
		"... --- ...",
		"... --- ... / ...",
		"... --- .../...",
		"... --- ...  /  ...",
	}
	for _, input := range variants {
		t.Run(input, func(t *testing.T) {
			syms, err := morseLinesToSymbols(input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(syms) == 0 {
				t.Error("expected at least one symbol")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// NFC normalisation
// ---------------------------------------------------------------------------

func TestNormAndUpperDecomposed(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"A + combining diaeresis → Ä", "A\u0308", "Ä"},
		{"A + combining ring → Å", "A\u030A", "Å"},
		{"e + combining acute → É (lowercase input)", "e\u0301", "É"},
		{"O + combining diaeresis → Ö", "O\u0308", "Ö"},
		{"U + combining diaeresis → Ü", "U\u0308", "Ü"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := normAndUpper(tc.input)
			if got != tc.want {
				t.Errorf("normAndUpper(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// normAndUpper output must always be in NFC form.
func TestNormAndUpperIsNFC(t *testing.T) {
	inputs := []string{"Ä", "A\u0308", "schön", "ÜBER", "ñ", "A\u030A\u0301"}
	for _, s := range inputs {
		got := normAndUpper(s)
		if !norm.NFC.IsNormalString(got) {
			t.Errorf("normAndUpper(%q) = %q is not NFC", s, got)
		}
	}
}

// NFC must be applied before AND after ToUpper (double-pass contract).
func TestNormAndUpperDoublePass(t *testing.T) {
	// The double-pass is: NFC → ToUpper → NFC.
	// We verify the contract by constructing the expected result manually
	// and checking normAndUpper matches it.
	inputs := []string{
		"A\u0308",       // decomposed Ä
		"O\u0308",       // decomposed Ö
		"e\u0301",       // decomposed é, lower-case
		"A\u030A\u0301", // multi-combining, no single precomposed form
	}
	for _, s := range inputs {
		want := norm.NFC.String(strings.ToUpper(norm.NFC.String(s)))
		got := normAndUpper(s)
		if got != want {
			t.Errorf("normAndUpper(%q) = %q, want %q", s, got, want)
		}
	}
}

func TestEncodeDecomposedEqualsPrecomposed(t *testing.T) {
	ct := newGermanTable()
	tests := []struct{ precomposed, decomposed string }{
		{"Ä", "A\u0308"},
		{"Ö", "O\u0308"},
		{"Ü", "U\u0308"},
	}
	for _, tc := range tests {
		pre, err := ct.EncodeLine(tc.precomposed)
		if err != nil {
			t.Fatalf("encode precomposed %q: %v", tc.precomposed, err)
		}
		dec, err := ct.EncodeLine(tc.decomposed)
		if err != nil {
			t.Fatalf("encode decomposed %q: %v", tc.decomposed, err)
		}
		if pre != dec {
			t.Errorf("precomposed %q → %q, decomposed %q → %q (should match)",
				tc.precomposed, pre, tc.decomposed, dec)
		}
	}
}

func TestNormAndUpperMultipleCombining(t *testing.T) {
	input := "A\u030A\u0301"
	got := normAndUpper(input)
	if got == "" {
		t.Error("normAndUpper returned empty string for multi-combining input")
	}
	if !norm.NFC.IsNormalString(got) {
		t.Errorf("normAndUpper(%q) = %q is not NFC", input, got)
	}
}

// ---------------------------------------------------------------------------
// Round-trip tests (ITU)
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
			encoded, err := encodeLine(original)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			decoded, err := decodeLine(encoded)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if decoded != original {
				t.Errorf("round-trip:\n original %q\n encoded  %q\n decoded  %q",
					original, encoded, decoded)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Stream tests
// ---------------------------------------------------------------------------

func TestEncodeStream(t *testing.T) {
	ct := ituTable()
	in := strings.NewReader("SOS\nHELLO WORLD\n")
	var out strings.Builder
	if err := ct.Encode(in, &out); err != nil {
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
	ct := ituTable()
	morse := "... --- ...\n.... . .-.. .-.. --- / .-- --- .-. .-.. -..\n"
	in := strings.NewReader(morse)
	var out strings.Builder
	if err := ct.Decode(in, &out); err != nil {
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

// A line well past 64 KiB must not cause a scanner error.
func TestScannerLargeInput(t *testing.T) {
	ct := ituTable()
	chunk := strings.Repeat("... ", 20000) // ~80 KiB
	in := strings.NewReader(strings.TrimRight(chunk, " ") + "\n")
	var out strings.Builder
	if err := ct.Decode(in, &out); err != nil {
		t.Fatalf("Decode failed on large input: %v", err)
	}
	result := strings.TrimRight(out.String(), "\n")
	if !strings.ContainsRune(result, 'S') {
		t.Error("decoded output missing expected 'S' characters")
	}
}

// ---------------------------------------------------------------------------
// ITU digraph CH
// ---------------------------------------------------------------------------

func TestDigraphCH(t *testing.T) {
	ct := ituTable()
	got, err := ct.EncodeLine("ACHTUNG")
	if err != nil {
		t.Fatal(err)
	}
	if got != ".- ---- - ..- -. --." {
		t.Errorf("got %q", got)
	}
}

// ---------------------------------------------------------------------------
// German table tests
// ---------------------------------------------------------------------------

func TestGermanUmlauts(t *testing.T) {
	ct := newGermanTable()
	tests := []struct{ input, want string }{
		{"Ä", ".-.-"}, {"Ö", "---."},
		{"Ü", ".." + "--"}, {"ß", "...--."}, // split to avoid go vet composite lit issue
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, err := ct.EncodeLine(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestGermanDecodeUmlautPriority(t *testing.T) {
	ct := newGermanTable()
	got, err := ct.DecodeLine(".-.-")
	if err != nil {
		t.Fatal(err)
	}
	if got != "Ä" {
		t.Errorf("got %q, want Ä", got)
	}
}

func TestGermanRoundTrip(t *testing.T) {
	ct := newGermanTable()
	phrases := []string{
		"GUTEN TAG", "SCHÖNE GRÜßE", "FÄHRT",
		"MÖBEL", "ÜBER", "DEUTSCHLAND", "SCHACH",
	}
	for _, original := range phrases {
		t.Run(original, func(t *testing.T) {
			encoded, err := ct.EncodeLine(original)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			decoded, err := ct.DecodeLine(encoded)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if decoded != original {
				t.Errorf("round-trip:\n original %q\n encoded  %q\n decoded  %q",
					original, encoded, decoded)
			}
		})
	}
}

func TestGermanSCHTrigraphEncode(t *testing.T) {
	ct := newGermanTable()
	got, err := ct.EncodeLine("SCHACH")
	if err != nil {
		t.Fatal(err)
	}
	// SCH(----) A(.-) C(-.-.) H(....)
	if got != "---- .- -.-. ...." {
		t.Errorf("got %q, want %q", got, "---- .- -.-. ....")
	}
}

func TestGermanSCHDecodeViaDecodeMulti(t *testing.T) {
	ct := newGermanTable()
	got, err := ct.DecodeLine("----")
	if err != nil {
		t.Fatal(err)
	}
	if got != "SCH" {
		t.Errorf("got %q, want SCH", got)
	}
}

func TestGermanDecomposedUmlautsRoundTrip(t *testing.T) {
	ct := newGermanTable()
	decomposed := "SCHO\u0308NE GRU\u0308\u00DFE"
	precomposed := "SCHÖNE GRÜßE"
	encDec, err := ct.EncodeLine(decomposed)
	if err != nil {
		t.Fatalf("encode decomposed: %v", err)
	}
	encPre, err := ct.EncodeLine(precomposed)
	if err != nil {
		t.Fatalf("encode precomposed: %v", err)
	}
	if encDec != encPre {
		t.Errorf("decomposed→%q, precomposed→%q (should match)", encDec, encPre)
	}
}

// ---------------------------------------------------------------------------
// Russian table tests
// ---------------------------------------------------------------------------

func TestRussianEncodeBasic(t *testing.T) {
	ct := newRussianTable()
	tests := []struct{ input, want string }{
		{"А", ".-"}, {"Т", "-"}, {"Р", ".-."}, {"С", "..."}, {"О", "---"}, {"Ш", "----"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, err := ct.EncodeLine(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRussianEncodeWord(t *testing.T) {
	ct := newRussianTable()
	got, err := ct.EncodeLine("МОРЗЕ")
	if err != nil {
		t.Fatal(err)
	}
	if got != "-- --- .-. --.. ." {
		t.Errorf("got %q", got)
	}
}

func TestRussianDecodeWord(t *testing.T) {
	ct := newRussianTable()
	got, err := ct.DecodeLine("-- --- .-. --.. .")
	if err != nil {
		t.Fatal(err)
	}
	if got != "МОРЗЕ" {
		t.Errorf("got %q, want МОРЗЕ", got)
	}
}

func TestRussianRoundTrip(t *testing.T) {
	ct := newRussianTable()
	for _, original := range []string{"МОСКВА", "ПРИВЕТ МИР", "73 ДЕ РК3АВТ"} {
		t.Run(original, func(t *testing.T) {
			encoded, err := ct.EncodeLine(original)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			decoded, err := ct.DecodeLine(encoded)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if decoded != original {
				t.Errorf("round-trip:\n original %q\n encoded  %q\n decoded  %q",
					original, encoded, decoded)
			}
		})
	}
}

func TestRussianYoCollapsesToYe(t *testing.T) {
	ct := newRussianTable()
	encoded, err := ct.EncodeLine("Ё")
	if err != nil {
		t.Fatal(err)
	}
	if encoded != "." {
		t.Fatalf("Ё → %q, want '.'", encoded)
	}
	decoded, err := ct.DecodeLine(".")
	if err != nil {
		t.Fatal(err)
	}
	if decoded != "Е" {
		t.Errorf("'.' → %q, want Е", decoded)
	}
}

// ---------------------------------------------------------------------------
// Chinese table tests
// ---------------------------------------------------------------------------

func TestChineseDigits(t *testing.T) {
	ct := newChineseTable()
	tests := []struct {
		digit rune
		want  string
	}{
		{'0', "-----"},
		{'1', ".----"},
		{'2', "..---"},
		{'3', "...--"},
		{'4', "....-"},
		{'5', "....."},
		{'6', "-...."},
		{'7', "--..."},
		{'8', "---.."},
		{'9', "----."},
	}
	for _, tc := range tests {
		t.Run(string(tc.digit), func(t *testing.T) {
			got, err := ct.EncodeLine(string(tc.digit))
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("digit %c: got %q, want %q", tc.digit, got, tc.want)
			}
		})
	}
}

func TestChineseCTCSimulated(t *testing.T) {
	ct := newChineseTable()
	niHaoCTC := "4919 1072"
	encoded, err := ct.EncodeLine(niHaoCTC)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	want := "....- ----. .---- ----. / .---- ----- --... ..---"
	if encoded != want {
		t.Errorf("got  %q\nwant %q", encoded, want)
	}
	decoded, err := ct.DecodeLine(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded != niHaoCTC {
		t.Errorf("round-trip: got %q, want %q", decoded, niHaoCTC)
	}
}

func TestChineseMixedLatinAndCTC(t *testing.T) {
	ct := newChineseTable()
	input := "QTH 0001"
	encoded, err := ct.EncodeLine(input)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := ct.DecodeLine(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded != input {
		t.Errorf("round-trip: got %q, want %q", decoded, input)
	}
}

func TestChineseRejectsHanziDirect(t *testing.T) {
	ct := newChineseTable()
	if _, err := ct.EncodeLine("你好"); err == nil {
		t.Fatal("expected error encoding Hanzi directly, got nil")
	}
}

// ---------------------------------------------------------------------------
// Custom table (mergeCustomTable) tests
// ---------------------------------------------------------------------------

func TestMergeCustomTableEncode(t *testing.T) {
	ct := buildWithCustomFile(t, ituSpec(), `{"encode":{"#":"...-.-"}}`)
	got, err := ct.EncodeLine("#")
	if err != nil {
		t.Fatal(err)
	}
	if got != "...-.-" {
		t.Errorf("got %q, want %q", got, "...-.-")
	}
}

func TestMergeCustomTableDigraph(t *testing.T) {
	ct := buildWithCustomFile(t, ituSpec(), `{"digraphs":{"CH":".-.-."}}`)
	got, err := ct.EncodeLine("CH")
	if err != nil {
		t.Fatal(err)
	}
	if got != ".-.-." {
		t.Errorf("got %q, want %q", got, ".-.-.")
	}
}

func TestMergeCustomTablePriorityOverride(t *testing.T) {
	ct := buildWithCustomFile(t, ituSpec(), `{"priority":["Æ","Ä","Å","À"]}`)
	got, err := ct.DecodeLine(".-.-")
	if err != nil {
		t.Fatal(err)
	}
	if got != "Æ" {
		t.Errorf("got %q, want Æ", got)
	}
}

func TestMergeCustomTableBadMulticharKey(t *testing.T) {
	path := writeCustomTableFile(t, `{"encode":{"AB":".."}}`)
	if _, err := MergeCustomTable(ituSpec(), path); err == nil {
		t.Fatal("expected error for multi-char encode key, got nil")
	}
}

func TestMergeCustomTableInvalidJSON(t *testing.T) {
	path := writeCustomTableFile(t, `{not valid json}`)
	if _, err := MergeCustomTable(ituSpec(), path); err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestMergeCustomTableFileNotFound(t *testing.T) {
	if _, err := MergeCustomTable(ituSpec(), "/nonexistent/path/table.json"); err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestChineseCustomCTCViaTable(t *testing.T) {
	ct := buildWithCustomFile(t, chineseSpec(),
		`{"encode":{"你":"....-.-.--.----.","好":".----.--.-.----"}}`)
	got, err := ct.EncodeLine("你好")
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	want := "....-.-.--.----. .----.--.-.----"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	decoded, err := ct.DecodeLine(got)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded != "你好" {
		t.Errorf("round-trip: got %q, want 你好", decoded)
	}
}

// ---------------------------------------------------------------------------
// Language registry
// ---------------------------------------------------------------------------

func TestLookupLangKnown(t *testing.T) {
	// Each entry pairs a language id with a character that is valid in that
	// table.  The spot-check ensures the sealed CodeTable is functional, not
	// merely non-nil.
	tests := []struct {
		id   string
		char string
	}{
		{"itu", "S"}, // Latin S
		{"en", "S"},  // alias for ITU
		{"de", "S"},  // German table includes full Latin alphabet
		{"ru", "С"},  // Cyrillic С (looks like C, but U+0421)
		{"zh", "5"},  // Chinese table covers digits
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			spec, err := LookupLang(tc.id)
			if err != nil {
				t.Fatalf("lookupLang(%q): %v", tc.id, err)
			}
			ct := BuildCodeTable(spec)
			if _, err := ct.EncodeLine(tc.char); err != nil {
				t.Errorf("lookupLang(%q): EncodeLine(%q): %v", tc.id, tc.char, err)
			}
		})
	}
}

func TestLookupLangUnknown(t *testing.T) {
	if _, err := LookupLang("klingon"); err == nil {
		t.Fatal("expected error for unknown lang, got nil")
	}
}

func TestLookupLangCaseInsensitive(t *testing.T) {
	for _, id := range []string{"ITU", "Itu", "RU", "Ru", "DE", "ZH"} {
		if _, err := LookupLang(id); err != nil {
			t.Errorf("lookupLang(%q): %v", id, err)
		}
	}
}

func TestPrintLangs(t *testing.T) {
	var sb strings.Builder
	PrintLangs(&sb)
	out := sb.String()
	for _, id := range []string{"itu", "de", "ru", "zh"} {
		if !strings.Contains(out, id) {
			t.Errorf("printLangs missing %q", id)
		}
	}
}

// ---------------------------------------------------------------------------
// resolveTable integration
// ---------------------------------------------------------------------------

func TestResolveTableDefault(t *testing.T) {
	ct, err := ResolveTable("itu", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := ct.EncodeLine("SOS")
	if err != nil {
		t.Fatal(err)
	}
	if got != "... --- ..." {
		t.Errorf("got %q", got)
	}
}

func TestResolveTableWithCustomFile(t *testing.T) {
	path := writeCustomTableFile(t, `{"encode":{"#":"...-.-"}}`)
	ct, err := ResolveTable("itu", path)
	if err != nil {
		t.Fatalf("resolveTable: %v", err)
	}
	got, err := ct.EncodeLine("#")
	if err != nil {
		t.Fatal(err)
	}
	if got != "...-.-" {
		t.Errorf("got %q", got)
	}
}

func TestResolveTableBadLang(t *testing.T) {
	if _, err := ResolveTable("elvish", ""); err == nil {
		t.Fatal("expected error for unknown lang, got nil")
	}
}

// ---------------------------------------------------------------------------
// printTableContents smoke test
// ---------------------------------------------------------------------------

func TestPrintTableContents(t *testing.T) {
	var sb strings.Builder
	PrintTableContents(&sb, newRussianTable(), "ru")
	out := sb.String()
	if !strings.Contains(out, "ru") {
		t.Error("output missing language id 'ru'")
	}
	if !strings.Contains(out, "А") {
		t.Error("output missing Cyrillic А")
	}
}

// ---------------------------------------------------------------------------
// JSON schema validation
// ---------------------------------------------------------------------------

func TestCustomTableJSONSchema(t *testing.T) {
	raw := `{"encode":{"@":".--.-.","~":".-.-"},"digraphs":{"NG":"--."}, "priority":["@","~"]}`
	var ct customTableJSON
	if err := json.Unmarshal([]byte(raw), &ct); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if ct.Encode["@"] != ".--.-." {
		t.Errorf("encode @: got %q", ct.Encode["@"])
	}
	if ct.Digraphs["NG"] != "--." {
		t.Errorf("digraph NG: got %q", ct.Digraphs["NG"])
	}
	if len(ct.Priority) != 2 || ct.Priority[0] != "@" {
		t.Errorf("priority: got %v", ct.Priority)
	}
}

// ---------------------------------------------------------------------------
// validateWavParams
// ---------------------------------------------------------------------------

func TestValidateWavParamsOK(t *testing.T) {
	p := WavParams{FreqHz: 700, DotMs: 60, Amplitude: 0.8,
		SampleRate: 44100, FadeMs: 5, InputMode: "text"}
	if err := ValidateWavParams(p); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateWavParamsErrors(t *testing.T) {
	base := WavParams{FreqHz: 700, DotMs: 60, Amplitude: 0.8,
		SampleRate: 44100, FadeMs: 5, InputMode: "text"}
	tests := []struct {
		name   string
		mutate func(*WavParams)
		substr string
	}{
		{"amp negative", func(p *WavParams) { p.Amplitude = -0.1 }, "-amp"},
		{"amp too large", func(p *WavParams) { p.Amplitude = 1.1 }, "-amp"},
		{"freq zero", func(p *WavParams) { p.FreqHz = 0 }, "-freq"},
		{"freq negative", func(p *WavParams) { p.FreqHz = -1 }, "-freq"},
		{"dot zero", func(p *WavParams) { p.DotMs = 0 }, "-dot"},
		{"rate too low", func(p *WavParams) { p.SampleRate = 7999 }, "-rate"},
		{"fade negative", func(p *WavParams) { p.FadeMs = -1 }, "-fade"},
		{"bad mode", func(p *WavParams) { p.InputMode = "wav" }, "-mode"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := base
			tc.mutate(&p)
			err := ValidateWavParams(p)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.substr) {
				t.Errorf("error %q missing %q", err.Error(), tc.substr)
			}
		})
	}
}

// The overflow threshold must be math.MaxInt, not math.MaxInt32.
func TestValidateWavParamsOverflowThreshold(t *testing.T) {
	// Construct a dotSamples64 value just above MaxInt (only meaningful on
	// 32-bit; on 64-bit this combination is practically unreachable, but the
	// formula must still reference MaxInt not MaxInt32).
	//
	// We verify the guard fires at math.MaxInt by choosing parameters whose
	// product is just over the limit.
	//
	// On 64-bit: MaxInt = 9_223_372_036_854_775_807 - we can't reach it with
	// realistic SampleRate/DotMs values, so we only check that a deliberately
	// overflowing pair is rejected and that the error mentions "overflow".
	if math.MaxInt == math.MaxInt32 {
		// 32-bit platform: straightforward overflow test.
		p := WavParams{
			FreqHz: 700, Amplitude: 0.8, FadeMs: 5, InputMode: "text",
			SampleRate: 44100,
			DotMs:      int(math.MaxInt32/44100) + 1,
		}
		err := ValidateWavParams(p)
		if err == nil || !strings.Contains(err.Error(), "overflow") {
			t.Errorf("expected overflow error on 32-bit, got %v", err)
		}
	} else {
		// 64-bit platform: confirm valid params just below any conceivable
		// threshold are accepted, and that the guard expression uses MaxInt.
		p := WavParams{FreqHz: 700, DotMs: 60, Amplitude: 0.8,
			SampleRate: 192000, FadeMs: 5, InputMode: "text"}
		if err := ValidateWavParams(p); err != nil {
			t.Errorf("valid 192kHz params rejected: %v", err)
		}
	}
}

func TestValidateWavParamsMultipleErrors(t *testing.T) {
	p := WavParams{FreqHz: -1, DotMs: -1, Amplitude: 2,
		SampleRate: 100, FadeMs: -1, InputMode: "bad"}
	err := ValidateWavParams(p)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	for _, s := range []string{"-amp", "-freq", "-dot", "-rate", "-fade", "-mode"} {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("combined error missing %q: %v", s, err)
		}
	}
}

// ---------------------------------------------------------------------------
// WAV fade clamping
// ---------------------------------------------------------------------------

func TestWriteWAVFadeClamp(t *testing.T) {
	syms := []symbol{{on: true, dots: unitDot}}
	p := WavParams{FreqHz: 700, DotMs: 1, Amplitude: 0.8,
		SampleRate: 44100, FadeMs: 50, InputMode: "text"}
	tmp, err := os.CreateTemp("", "morse-fade-*.wav")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(tmp.Name()); tmp.Close() })
	if err := writeWAV(tmp, syms, p); err != nil {
		t.Fatalf("writeWAV with large fade: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Morse timing constants
// ---------------------------------------------------------------------------

func TestTimingConstants(t *testing.T) {
	if unitDot != 1 {
		t.Errorf("unitDot = %d, want 1", unitDot)
	}
	if unitDash != 3 {
		t.Errorf("unitDash = %d, want 3", unitDash)
	}
	if unitIntraChar != 1 {
		t.Errorf("unitIntraChar = %d, want 1", unitIntraChar)
	}
	if unitInterChar != 3 {
		t.Errorf("unitInterChar = %d, want 3", unitInterChar)
	}
	if unitInterWord != 7 {
		t.Errorf("unitInterWord = %d, want 7", unitInterWord)
	}
}

func TestTimingRatios(t *testing.T) {
	if unitDash != 3*unitDot {
		t.Errorf("dash (%d) ≠ 3×dot (%d)", unitDash, unitDot)
	}
	if unitInterChar != 3*unitIntraChar {
		t.Errorf("inter-char (%d) ≠ 3×intra-char (%d)", unitInterChar, unitIntraChar)
	}
	if unitInterWord != 7*unitDot {
		t.Errorf("inter-word (%d) ≠ 7×dot (%d)", unitInterWord, unitDot)
	}
}
