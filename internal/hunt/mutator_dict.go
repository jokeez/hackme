package hunt

import "strings"

var (
	huntDictJSON    = []byte(`{}[]":,nulltruefalseNaNInfinitynumber\x00\xff\u0000\u`)
	huntDictXML     = []byte(`<>&lt;&gt;&amp;&quot;&apos;CDATA<?xml"'=/><!--`)
	huntDictINI     = []byte("#=\n\r\t[section]key=value;truefalseyesno")
	huntDictTOML    = []byte("#=\n[]key=value\"\"''truefalse1970-01-01T00:00:00Z")
	huntDictMsg     = []byte("\x80\x81\x82\x83\x84\x85\x86\x87\x88\x89\x90\x91\xdc\xdd\xde\xdf\xc0\xc2\xc3")
	huntDictGeneric = []byte("truefalseNaNnull%n%s../2147483648-1\x00\xff")
)

var huntJSONTargets = map[string]struct{}{
	"jsmn": {}, "cjson": {}, "parson": {}, "yyjson": {}, "jansson": {},
	"centijson": {}, "cj5": {}, "mjson": {}, "sheredom": {}, "cfgpack": {},
	"frozen": {}, "mpack": {}, "libcbor": {}, "tinycbor": {}, "jsonparser": {},
	"json-c": {}, "spl": {},
}

var huntXMLTargets = map[string]struct{}{
	"expat": {}, "mxml": {}, "cmark": {}, "libxml2": {},
}

var huntINITargets = map[string]struct{}{
	"inih": {}, "libconfini": {}, "libucl": {},
}

var huntTOMLTargets = map[string]struct{}{
	"tomlc99": {}, "tomlc17": {}, "cyaml": {},
}

// MutatorDictForTarget returns domain splice dictionary bytes for a catalog target.
func MutatorDictForTarget(targetID string) []byte {
	id := strings.TrimSpace(strings.ToLower(targetID))
	if id == "" {
		return nil
	}
	if _, ok := huntJSONTargets[id]; ok {
		return append([]byte(nil), huntDictJSON...)
	}
	if _, ok := huntXMLTargets[id]; ok {
		return append([]byte(nil), huntDictXML...)
	}
	if _, ok := huntINITargets[id]; ok {
		return append([]byte(nil), huntDictINI...)
	}
	if _, ok := huntTOMLTargets[id]; ok {
		return append([]byte(nil), huntDictTOML...)
	}
	if strings.Contains(id, "msgpack") || id == "cfgpack" {
		return append([]byte(nil), huntDictMsg...)
	}
	// Parser-class inventory ids: blend JSON + generic text tokens.
	if strings.HasPrefix(id, "inv_") || strings.Contains(id, "json") || strings.Contains(id, "parse") {
		return append([]byte(nil), huntDictJSON...)
	}
	return append([]byte(nil), huntDictGeneric...)
}

// ApplyHuntMutatorDict sets mutator_dict when not already present.
func ApplyHuntMutatorDict(cfg map[string]any, targetID string) {
	if cfg == nil {
		return
	}
	if _, ok := cfg["mutator_dict"]; ok {
		return
	}
	if d := MutatorDictForTarget(targetID); len(d) > 0 {
		cfg["mutator_dict"] = d
		cfg["hunt_mutator_profile"] = mutatorProfileForTarget(targetID)
	}
}

func mutatorProfileForTarget(targetID string) string {
	id := strings.TrimSpace(strings.ToLower(targetID))
	switch {
	case containsKey(huntJSONTargets, id):
		return "json"
	case containsKey(huntXMLTargets, id):
		return "xml"
	case containsKey(huntINITargets, id):
		return "ini"
	case containsKey(huntTOMLTargets, id):
		return "toml"
	case strings.Contains(id, "msgpack") || id == "cfgpack":
		return "msgpack"
	default:
		return "generic"
	}
}

func containsKey(m map[string]struct{}, id string) bool {
	_, ok := m[id]
	return ok
}

// Profile libFuzzer dictionaries (quoted tokens / \x lines). Byte mutator_dict
// stays the flat splice blob; -dict= needs this grammar or libFuzzer rejects it.
var (
	lfTokensJSON = []string{"null", "true", "false", "NaN", "Infinity", "{}", "[]", "key", "0", "1e309", "\"", "\\u0000"}
	lfTokensXML  = []string{"<?xml", "CDATA", "<![CDATA[", "]]>", "&lt;", "&gt;", "&amp;", "&quot;", "<!--", "-->"}
	lfTokensINI  = []string{"[section]", "key=value", "true", "false", "yes", "no"}
	lfTokensTOML = []string{"[table]", "key=value", "true", "false", "1970-01-01T00:00:00Z"}
	lfTokensMsg  = []string{`\x80`, `\xc0`, `\xdc\x00\x01`, `\xdd\x00\x00\x00\x01`, `\xde`}
	lfTokensGen  = []string{"%n", "%s", "../", "2147483648", "-1", "4294967295", "NaN", "null", `\x00`, `\xff`, "A" + strings.Repeat("A", 16)}
)

// LibFuzzerDictTextForTarget is a -dict= file body for catalog and inventory ids.
// Unknown targets still get a generic boundary dict so the fleet is not dict-less.
func LibFuzzerDictTextForTarget(targetID string) string {
	if strings.TrimSpace(targetID) == "" {
		return ""
	}
	var toks []string
	switch mutatorProfileForTarget(targetID) {
	case "json":
		toks = lfTokensJSON
	case "xml":
		toks = lfTokensXML
	case "ini":
		toks = lfTokensINI
	case "toml":
		toks = lfTokensTOML
	case "msgpack":
		toks = lfTokensMsg
	default:
		// inv_* / *json* / *parse* use the JSON byte dict but profile "generic"
		// unless they hit a named map. Prefer JSON tokens for those ids.
		id := strings.TrimSpace(strings.ToLower(targetID))
		if strings.HasPrefix(id, "inv_") || strings.Contains(id, "json") || strings.Contains(id, "parse") {
			toks = lfTokensJSON
		} else {
			toks = lfTokensGen
		}
	}
	return formatLibFuzzerDict(toks)
}

// EnsureLibFuzzerDict stamps libfuzzer_dict when the campaign has none.
// Inventory pack *.dict (already applied) is left untouched.
func EnsureLibFuzzerDict(cfg map[string]any, targetID string) {
	if cfg == nil {
		return
	}
	if len(LibFuzzerDictFromConfig(cfg)) > 0 {
		return
	}
	if text := LibFuzzerDictTextForTarget(targetID); text != "" {
		cfg["libfuzzer_dict"] = text
	}
}

func formatLibFuzzerDict(tokens []string) string {
	var b strings.Builder
	b.WriteString("# hackme hunt profile dict\n")
	seen := map[string]struct{}{}
	for _, t := range tokens {
		t = strings.TrimSpace(t)
		if t == "" || strings.ContainsAny(t, "\n\r") || len(t) > 64 {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		if strings.HasPrefix(t, `\x`) {
			// libFuzzer's ParseDictionaryFile only accepts a quoted value: a bare
			// \x00 line aborts the whole dictionary and the session exits before it
			// runs ("ParseDictionaryFile: error in line N", cov 0, 0 execs). Hex
			// escapes are interpreted inside the quotes, so quote without escaping
			// the backslash.
			b.WriteByte('"')
			b.WriteString(t)
			b.WriteByte('"')
			b.WriteByte('\n')
			continue
		}
		esc := strings.ReplaceAll(t, `\`, `\\`)
		esc = strings.ReplaceAll(esc, `"`, `\"`)
		b.WriteByte('"')
		b.WriteString(esc)
		b.WriteByte('"')
		b.WriteByte('\n')
	}
	return b.String()
}
