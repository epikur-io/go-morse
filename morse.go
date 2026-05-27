// morse - ITU-R M.1677-1 compliant Morse code encoder / decoder / WAV generator
package morse

import (
	"flag"
)

// ---------------------------------------------------------------------------
// ITU-R M.1677-1 timing constants (all values in dot units)
// ---------------------------------------------------------------------------

const (
	unitDot       = 1 // dot duration
	unitDash      = 3 // dash duration
	unitIntraChar = 1 // gap between elements within one character
	unitInterChar = 3 // gap between characters within one word
	unitInterWord = 7 // gap between words
)

func AddLangFlags(fs *flag.FlagSet) (*string, *string) {
	lang := fs.String("lang", "itu", `language table id (run "morse langs" to list)`)
	table := fs.String("table", "", "JSON file with custom mappings (merged on top of -lang)")
	return lang, table
}

func ResolveTable(langID, tableFile string) (*CodeTable, error) {
	spec, err := LookupLang(langID)
	if err != nil {
		return nil, err
	}
	if tableFile != "" {
		spec, err = MergeCustomTable(spec, tableFile)
		if err != nil {
			return nil, err
		}
	}
	return BuildCodeTable(spec), nil
}
