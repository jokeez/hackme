package hunt

import "strings"

// Format grammars for targets whose own sources yield few or no string literals.
//
// A single-file parser like zserge/jsmn has almost no string constants, so
// DeriveDictTokensFromSources returns nothing and the dictionary falls back to a scan of
// the harness binary — which for jsmn and cj5 is 2.8 KB of unrelated symbol noise, the
// worst possible dictionary for a JSON tokenizer. Measured 2026-11-25: jsmn 0 source
// tokens, uriparser 4, cj5 0.
//
// The fix is to recognise the input format and seed the structural tokens the format is
// built from. These are universal to the format, not to any one library, so they help
// whatever parser reads it. The grammar is conservative in two ways: a format is only
// claimed on an unambiguous signal (see classifyTargetFormat), and each profile is a
// dozen-odd tokens — if a classification were ever wrong the cost is that handful, not a
// whole second program's vocabulary.

// formatGrammar maps a detected format to its structural tokens. Text formats list the
// bytes and keywords a parser branches on; binary formats list magic and type markers as
// \x escapes (formatLibFuzzerDict quotes these).
var formatGrammar = map[string][]string{
	"json": {
		"{", "}", "[", "]", ":", ",", "\"", "true", "false", "null",
		"\\u0000", "\\uffff", "\\n", "\\t", "\\\\", "\\\"", "0", "-0", "1e10", "1E-10",
	},
	"json5": { // superset of JSON: comments, unquoted keys, hex, trailing commas
		"{", "}", "[", "]", ":", ",", "'", "\"", "true", "false", "null",
		"//", "/*", "*/", "0x", "Infinity", "NaN", "+", ".5",
	},
	"xml": {
		"<", ">", "</", "/>", "<?xml", "?>", "<!--", "-->", "<![CDATA[", "]]>",
		"<!DOCTYPE", "&amp;", "&lt;", "&gt;", "&quot;", "&#", "=\"", "xmlns",
	},
	"toml": {
		"[", "]", "[[", "]]", "=", "\"", "'", "\"\"\"", "'''", ".",
		"true", "false", "#", "+", "-", ":", "T", "Z",
	},
	"yaml": {
		":", "- ", "---", "...", "?", "|", ">", "#", "&", "*", "!!",
		"'", "\"", "true", "false", "null", "~", "[]", "{}",
	},
	"cbor": { // RFC 8949 major types / breaks / tags, as leading bytes
		"\\x00", "\\x20", "\\x40", "\\x60", "\\x80", "\\xa0", "\\xc0", "\\xe0",
		"\\xf4", "\\xf5", "\\xf6", "\\xf7", "\\xff", "\\x18", "\\x19", "\\x1a", "\\x1b",
	},
	"msgpack": { // fixmap/fixarray/fixstr and the typed markers
		"\\x80", "\\x90", "\\xa0", "\\xc0", "\\xc2", "\\xc3", "\\xcc", "\\xcd",
		"\\xce", "\\xcf", "\\xdc", "\\xdd", "\\xde", "\\xdf", "\\xd9",
	},
}

// classifyTargetFormat names the input format of a catalog target from its id and title,
// or "" when nothing is unambiguous. Conservative on purpose: a wrong grammar is noise,
// so silence is preferred over a guess. srcHasJSONWords lets a strong source signal
// confirm JSON when the id/title is generic.
func classifyTargetFormat(targetID, title string, srcTokens []string) string {
	hay := strings.ToLower(targetID + " " + title)

	// Order matters: json5 and msgpack must win over the substrings they contain.
	switch {
	case strings.Contains(hay, "json5") || strings.Contains(hay, "cj5"):
		return "json5"
	case strings.Contains(hay, "msgpack") || strings.Contains(hay, "mpack"):
		return "msgpack"
	case strings.Contains(hay, "cbor"):
		return "cbor"
	case strings.Contains(hay, "toml"):
		return "toml"
	case strings.Contains(hay, "yaml") || strings.Contains(hay, "yml"):
		return "yaml"
	case strings.Contains(hay, "xml"):
		return "xml"
	case strings.Contains(hay, "json") || strings.Contains(hay, "jsmn"):
		return "json"
	}
	// No format word in id/title. Only claim JSON if the sources themselves carry its
	// keywords together — e.g. a parser named after its author, not its format.
	if srcHasJSONWords(srcTokens) {
		return "json"
	}
	return ""
}

func srcHasJSONWords(tokens []string) bool {
	need := map[string]bool{"null": false, "true": false, "false": false}
	for _, t := range tokens {
		if _, ok := need[strings.ToLower(strings.TrimSpace(t))]; ok {
			need[strings.ToLower(strings.TrimSpace(t))] = true
		}
	}
	return need["null"] && need["true"] && need["false"]
}

// FormatGrammarTokens returns the structural tokens for a target's input format, or nil
// when the format is not recognised. The result is meant to be merged with source-derived
// tokens, never to replace them.
func FormatGrammarTokens(targetID, title string, srcTokens []string) (tokens []string, format string) {
	format = classifyTargetFormat(targetID, title, srcTokens)
	if format == "" {
		return nil, ""
	}
	g := formatGrammar[format]
	return append([]string(nil), g...), format
}
