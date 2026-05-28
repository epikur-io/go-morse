package morse

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

type tableSpec struct {
	encodeMap              map[rune]string
	digraphs               map[string]string
	decodePriority         []rune
	combiningToPrecomposed map[string]rune // documentation only; not used at runtime
}

// clone returns a fully independent deep copy of s.
func (s tableSpec) clone() tableSpec {
	return tableSpec{
		encodeMap:              cloneRuneStringMap(s.encodeMap),
		digraphs:               cloneStringStringMap(s.digraphs),
		decodePriority:         cloneRunes(s.decodePriority),
		combiningToPrecomposed: cloneStringRuneMap(s.combiningToPrecomposed),
	}
}

func cloneRuneStringMap(m map[rune]string) map[rune]string {
	out := make(map[rune]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func cloneStringStringMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func cloneStringRuneMap(m map[string]rune) map[string]rune {
	out := make(map[string]rune, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func cloneRunes(s []rune) []rune {
	out := make([]rune, len(s))
	copy(out, s)
	return out
}

// CodeTable is safe for concurrent use. All fields are set once by
// buildCodeTable and are never written again.
type CodeTable struct {
	encodeMap   map[rune]string
	digraphs    map[string]string
	decodeMap   map[string]rune
	decodeMulti map[string]string

	// maxDigraphRunes is the length of the longest key in digraphs, cached at
	// construction time to bound the inner loop in EncodeLine without repeated
	// utf8.RuneCountInString calls.
	maxDigraphRunes int
}

// BuildCodeTable derives decode maps and seals an immutable CodeTable.
// It deep-copies encodeMap and digraphs from spec.
func BuildCodeTable(spec tableSpec) *CodeTable {
	encodeMap := cloneRuneStringMap(spec.encodeMap)
	digraphs := cloneStringStringMap(spec.digraphs)

	// Priority map - first occurrence wins; duplicates ignored.
	prio := make(map[rune]int, len(spec.decodePriority))
	for i, r := range spec.decodePriority {
		if _, already := prio[r]; !already {
			prio[r] = i
		}
	}
	lowPrio := len(spec.decodePriority)

	dm := make(map[string]rune, len(encodeMap))
	for r, code := range encodeMap {
		existing, collision := dm[code]
		if !collision {
			dm[code] = r
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
			dm[code] = r
		}
	}

	multi := make(map[string]string)
	maxDigraphRunes := 0
	for text, code := range digraphs {
		n := utf8.RuneCountInString(text)
		if n > 1 {
			multi[code] = text
		}
		if n > maxDigraphRunes {
			maxDigraphRunes = n
		}
	}

	return &CodeTable{
		encodeMap:       encodeMap,
		digraphs:        digraphs,
		decodeMap:       dm,
		decodeMulti:     multi,
		maxDigraphRunes: maxDigraphRunes,
	}
}

// normAndUpper converts s to upper-case with correct Unicode normalisation.
//
// Order of operations:
//  1. NFC-normalise so ToUpper operates on canonical forms.
//  2. ToUpper - case conversion can alter normalisation form.
//  3. NFC-normalise again to restore canonical form after casing.
func normAndUpper(s string) string {
	return norm.NFC.String(strings.ToUpper(norm.NFC.String(s)))
}

// EncodeLine encodes one line of plain text to Morse.
// Digraphs/trigraphs are matched longest-first using the precomputed
// maxDigraphRunes bound, avoiding unnecessary iterations and substring
// allocations.
func (ct *CodeTable) EncodeLine(line string) (string, error) {
	var words []string
	for _, word := range strings.Fields(line) {
		upper := normAndUpper(word)
		runes := []rune(upper)
		tokens := make([]string, 0, len(runes))
		for i := 0; i < len(runes); i++ {
			// Longest-match digraph search, bounded by the precomputed maximum.
			maxLen := len(runes) - i
			if maxLen > ct.maxDigraphRunes {
				maxLen = ct.maxDigraphRunes
			}
			matched := false
			for l := maxLen; l >= 2; l-- {
				if code, ok := ct.digraphs[string(runes[i:i+l])]; ok {
					tokens = append(tokens, code)
					i += l - 1
					matched = true
					break
				}
			}
			if matched {
				continue
			}
			code, ok := ct.encodeMap[runes[i]]
			if !ok {
				return "", fmt.Errorf("unsupported character: %q (U+%04X)", runes[i], runes[i])
			}
			tokens = append(tokens, code)
		}
		if len(tokens) > 0 {
			words = append(words, strings.Join(tokens, " "))
		}
	}
	return strings.Join(words, " / "), nil
}

// DecodeLine decodes one line of Morse to plain text.
//
// Word separator: split on "/" with whitespace trimmed around each segment,
// accepting " / ", "/", "  /  ", tabs, etc.
func (ct *CodeTable) DecodeLine(line string) (string, error) {
	var sb strings.Builder
	for wi, seg := range strings.Split(line, "/") {
		if wi > 0 {
			sb.WriteRune(' ')
		}
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		for _, token := range strings.Fields(seg) {
			if multi, ok := ct.decodeMulti[token]; ok {
				sb.WriteString(multi)
				continue
			}
			r, ok := ct.decodeMap[token]
			if !ok {
				return "", fmt.Errorf("unknown Morse sequence: %q", token)
			}
			sb.WriteRune(r)
		}
	}
	return sb.String(), nil
}

// newScanner returns a bufio.Scanner with a 1 MiB line buffer.
func newScanner(r io.Reader) *bufio.Scanner {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 64*1024), 1024*1024)
	return s
}

// Encode reads plain text from r and writes Morse to w.
// Flush errors from the buffered writer are returned via the named return.
func (ct *CodeTable) Encode(r io.Reader, w io.Writer) (err error) {
	scanner := newScanner(r)
	bw := bufio.NewWriter(w)
	defer func() {
		if ferr := bw.Flush(); err == nil && ferr != nil {
			err = ferr
		}
	}()
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			fmt.Fprintln(bw)
			continue
		}
		var encoded string
		encoded, err = ct.EncodeLine(line)
		if err != nil {
			return
		}
		fmt.Fprintln(bw, encoded)
	}
	if err = scanner.Err(); err != nil {
		return
	}
	return
}

// Decode reads Morse from r and writes plain text to w.
// Flush errors from the buffered writer are returned via the named return.
func (ct *CodeTable) Decode(r io.Reader, w io.Writer) (err error) {
	scanner := newScanner(r)
	bw := bufio.NewWriter(w)
	defer func() {
		if ferr := bw.Flush(); err == nil && ferr != nil {
			err = ferr
		}
	}()
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			fmt.Fprintln(bw)
			continue
		}
		var decoded string
		decoded, err = ct.DecodeLine(line)
		if err != nil {
			return
		}
		fmt.Fprintln(bw, decoded)
	}
	if err = scanner.Err(); err != nil {
		return
	}
	return
}

var defaultTable = newITUTable()

func encodeLine(line string) (string, error) { return defaultTable.EncodeLine(line) }
func decodeLine(line string) (string, error) { return defaultTable.DecodeLine(line) }

type customTableJSON struct {
	Encode   map[string]string `json:"encode"`
	Digraphs map[string]string `json:"digraphs"`
	Priority []string          `json:"priority"`
}

// MergeCustomTable loads the JSON file at path, merges it into a clone of
// spec, and returns the modified clone. The input spec is never mutated.
func MergeCustomTable(spec tableSpec, path string) (tableSpec, error) {
	out := spec.clone()

	f, err := os.Open(path)
	if err != nil {
		return out, fmt.Errorf("open custom table: %w", err)
	}
	defer f.Close()

	var ct customTableJSON
	if err := json.NewDecoder(f).Decode(&ct); err != nil {
		return out, fmt.Errorf("parse custom table %s: %w", path, err)
	}
	for ks, code := range ct.Encode {
		runes := []rune(ks)
		if len(runes) != 1 {
			return out, fmt.Errorf("custom table: encode key %q must be a single character", ks)
		}
		out.encodeMap[unicode.ToUpper(runes[0])] = code
	}
	for digraph, code := range ct.Digraphs {
		out.digraphs[strings.ToUpper(digraph)] = code
	}
	if len(ct.Priority) > 0 {
		out.decodePriority = nil
		for _, s := range ct.Priority {
			runes := []rune(s)
			if len(runes) != 1 {
				return out, fmt.Errorf("custom table: priority entry %q must be a single character", s)
			}
			out.decodePriority = append(out.decodePriority, unicode.ToUpper(runes[0]))
		}
	}
	return out, nil
}
