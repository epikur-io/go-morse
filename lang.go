package morse

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// ---------------------------------------------------------------------------
// Language registry
// ---------------------------------------------------------------------------

type langEntry struct {
	id          string
	description string
	specFn      func() tableSpec
}

var langRegistry = []langEntry{
	{"itu", "ITU-R M.1677-1 (Latin + extended European, default)", ituSpec},
	{"en", "English – alias for the ITU Latin table", ituSpec},
	{"de", "German (DARC) – ITU Latin + Ä Ö Ü ß + SCH trigraph, no CH digraph", germanSpec},
	{"ru", "Russian / Cyrillic (DOSAAF) – full Cyrillic alphabet", russianSpec},
	{"zh", "Chinese – digit/Latin table for Continental Telegraph Code (CTC) numerics", chineseSpec},
}

func LookupLang(id string) (tableSpec, error) {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, e := range langRegistry {
		if e.id == id {
			return e.specFn(), nil
		}
	}
	var ids []string
	for _, e := range langRegistry {
		ids = append(ids, e.id)
	}
	return tableSpec{}, fmt.Errorf("unknown language %q; available: %s", id, strings.Join(ids, ", "))
}

func PrintLangs(w io.Writer) {
	fmt.Fprintln(w, "Built-in language tables:")
	fmt.Fprintln(w)
	maxID := 0
	for _, e := range langRegistry {
		if len(e.id) > maxID {
			maxID = len(e.id)
		}
	}
	for _, e := range langRegistry {
		fmt.Fprintf(w, "  %-*s  %s\n", maxID, e.id, e.description)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, `Use -lang <id> to select a table.`)
	fmt.Fprintln(w, `Use -table <file> to merge a custom JSON table on top of the selected language.`)
}

func PrintTableContents(w io.Writer, ct *CodeTable, langID string) {
	fmt.Fprintf(w, "Encode table for language %q (%d entries):\n\n", langID, len(ct.encodeMap))
	type kv struct {
		r    rune
		code string
	}
	entries := make([]kv, 0, len(ct.encodeMap))
	for r, code := range ct.encodeMap {
		entries = append(entries, kv{r, code})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].r < entries[j].r })
	for _, e := range entries {
		fmt.Fprintf(w, "  U+%04X  %c  %s\n", e.r, e.r, e.code)
	}
	if len(ct.digraphs) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Digraphs / trigraphs:")
		digs := make([]string, 0, len(ct.digraphs))
		for k := range ct.digraphs {
			digs = append(digs, k)
		}
		sort.Strings(digs)
		for _, d := range digs {
			fmt.Fprintf(w, "  %s  %s\n", d, ct.digraphs[d])
		}
	}
	if len(ct.decodeMulti) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Multi-character decode overrides:")
		codes := make([]string, 0, len(ct.decodeMulti))
		for code := range ct.decodeMulti {
			codes = append(codes, code)
		}
		sort.Strings(codes)
		for _, code := range codes {
			fmt.Fprintf(w, "  %s  → %q\n", code, ct.decodeMulti[code])
		}
	}
}
