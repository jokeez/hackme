package poolfuzz

import (
	"encoding/json"
	"strings"
)

func parseConfigJSON(raw string) map[string]any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil || m == nil {
		return map[string]any{}
	}
	return m
}

func marshalConfigJSON(m map[string]any) string {
	if m == nil {
		return "{}"
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func marshalSummaryJSON(m map[string]any) string {
	return marshalConfigJSON(m)
}

// PoolDistributed reports whether the campaign runs on coordinator pool workers.
func PoolDistributed(cfg map[string]any) bool {
	return poolDistributed(cfg)
}

func poolDistributed(cfg map[string]any) bool {
	if cfg == nil {
		return false
	}
	for _, k := range []string{"pool_distributed", "pool_workers", "distributed_pool"} {
		v := strings.TrimSpace(strings.ToLower(jsonString(cfg[k])))
		if v == "1" || v == "true" || v == "yes" || v == "on" {
			return true
		}
	}
	return false
}

func jsonString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	default:
		return ""
	}
}
