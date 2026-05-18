package chain

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// TaskKind identifies how work is evaluated (extensible for future WASM packs).
type TaskKind string

const (
	// TaskKindSyntheticPoH is the built-in eval(n)=n*7+13 PoH checked against chain meta poh_target_mod.
	TaskKindSyntheticPoH TaskKind = "synthetic_poh_v1"
	// TaskKindSyntheticPoHV2 is the next built-in PoH profile (deterministic mixed arithmetic).
	TaskKindSyntheticPoHV2 TaskKind = "synthetic_poh_v2"
)

// TaskSpec is a portable manifest snapshot (file or internal). Zero RewardHMC means "use miner default".
type TaskSpec struct {
	ID           string        `json:"id"`
	Kind         TaskKind      `json:"kind"`
	ArtifactHash string        `json:"artifact_hash,omitempty"`
	Timeout      time.Duration `json:"-"`
	TimeoutMS    int64         `json:"timeout_ms,omitempty"`
	RewardHMC    float64       `json:"reward_hmc,omitempty"`
	Source       string        `json:"source"` // "internal" | "file" | "order" (SQLite POST /api/tasks)
	ManifestPath string        `json:"manifest_path,omitempty"`
	// WasmCheck, when non-nil, requires an extra WASM export check(i64)->i32 (non-zero = pass) after native PoH hit.
	WasmCheck []byte `json:"-"`
}

// TaskProvider supplies the active task manifest for logging, metrics, and future executors.
type TaskProvider interface {
	Snapshot(ctx context.Context) (TaskSpec, error)
}

// InternalTaskProvider is the default: synthetic PoH wired in the binary.
type InternalTaskProvider struct{}

// Snapshot returns the built-in synthetic task.
func (InternalTaskProvider) Snapshot(context.Context) (TaskSpec, error) {
	return TaskSpec{
		ID:     "internal_synthetic_poh",
		Kind:   TaskKindSyntheticPoH,
		Source: "internal",
	}, nil
}

// FileTaskProvider loads the newest *.json manifest from Dir (non-recursive).
// If the directory is missing, empty, or no valid manifest, Fallback is used.
type FileTaskProvider struct {
	Dir      string
	Fallback TaskProvider
	TTL      time.Duration

	mu       sync.Mutex
	cached   TaskSpec
	cachedAt time.Time
}

// NewFileTaskProvider returns a provider that prefers manifests in dir.
func NewFileTaskProvider(dir string, fallback TaskProvider) *FileTaskProvider {
	if fallback == nil {
		fallback = InternalTaskProvider{}
	}
	return &FileTaskProvider{
		Dir:      dir,
		Fallback: fallback,
		TTL:      2 * time.Second,
	}
}

// Snapshot returns cached spec if fresh; otherwise rescans the directory.
func (f *FileTaskProvider) Snapshot(ctx context.Context) (TaskSpec, error) {
	if f.Dir == "" {
		return f.Fallback.Snapshot(ctx)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.TTL > 0 && !f.cachedAt.IsZero() && time.Since(f.cachedAt) < f.TTL {
		return f.cached, nil
	}

	select {
	case <-ctx.Done():
		return TaskSpec{}, ctx.Err()
	default:
	}

	spec, err := f.scanUnlocked()
	if err != nil || spec.ID == "" {
		s2, ferr := f.Fallback.Snapshot(ctx)
		if ferr != nil {
			if err != nil {
				return TaskSpec{}, err
			}
			return TaskSpec{}, ferr
		}
		f.cached = s2
		f.cachedAt = time.Now()
		return s2, nil
	}

	f.cached = spec
	f.cachedAt = time.Now()
	return spec, nil
}

func (f *FileTaskProvider) scanUnlocked() (TaskSpec, error) {
	entries, err := os.ReadDir(f.Dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return TaskSpec{}, nil
		}
		return TaskSpec{}, err
	}

	type cand struct {
		path string
		info os.FileInfo
	}
	var cands []cand
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(name), ".json") {
			continue
		}
		full := filepath.Join(f.Dir, name)
		st, err := os.Stat(full)
		if err != nil || !st.Mode().IsRegular() {
			continue
		}
		cands = append(cands, cand{path: full, info: st})
	}
	if len(cands) == 0 {
		return TaskSpec{}, nil
	}
	sort.Slice(cands, func(i, j int) bool {
		return cands[i].info.ModTime().After(cands[j].info.ModTime())
	})

	for _, c := range cands {
		raw, err := os.ReadFile(c.path)
		if err != nil {
			continue
		}
		var m fileManifestJSON
		if err := json.Unmarshal(raw, &m); err != nil {
			continue
		}
		if strings.TrimSpace(m.ID) == "" {
			continue
		}
		kind := TaskKind(m.Kind)
		if kind == "" {
			kind = TaskKindSyntheticPoH
		}
		if kind != TaskKindSyntheticPoH {
			// Only built-in PoH is executable today; skip unsupported manifests.
			continue
		}
		spec := TaskSpec{
			ID:           strings.TrimSpace(m.ID),
			Kind:         kind,
			ArtifactHash: strings.TrimSpace(m.ArtifactHash),
			RewardHMC:    m.RewardHMC,
			Source:       "file",
			ManifestPath: c.path,
			TimeoutMS:    m.TimeoutMS,
		}
		if m.TimeoutMS > 0 {
			spec.Timeout = time.Duration(m.TimeoutMS) * time.Millisecond
		}
		if wb, err := ResolveWasmCheckFromManifest(raw, DefaultArtifactRoot()); err != nil {
			return TaskSpec{}, err
		} else {
			spec.WasmCheck = wb
		}
		return spec, nil
	}
	return TaskSpec{}, nil
}

type fileManifestJSON struct {
	ID           string  `json:"id"`
	Kind         string  `json:"kind"`
	ArtifactHash string  `json:"artifact_hash"`
	TimeoutMS    int64   `json:"timeout_ms"`
	RewardHMC    float64 `json:"reward_hmc"`
	WasmCheckHex string  `json:"wasm_check_hex"`
}
