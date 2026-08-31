package hunt

import "strings"

var (
	huntDictJSON = []byte(`{}[]":,nulltruefalsenumber\x00\xff\u`)
	huntDictXML  = []byte(`<>&lt;&gt;&amp;CDATA<?xml"'=/>`)
	huntDictINI  = []byte("#=\n\r\t[section]key=value;")
	huntDictTOML = []byte("#=\n[]key=value\"\"''")
	huntDictMsg  = []byte("\x80\x81\x82\x83\x84\x85\x86\x87\x88\x89\xde\xdf")
)

var huntJSONTargets = map[string]struct{}{
	"jsmn": {}, "cjson": {}, "parson": {}, "yyjson": {}, "jansson": {},
	"centijson": {}, "cj5": {}, "mjson": {}, "sheredom": {}, "cfgpack": {},
	"frozen": {}, "mpack": {}, "libcbor": {}, "tinycbor": {}, "jsonparser": {},
	"json-c": {},
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
	return nil
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
