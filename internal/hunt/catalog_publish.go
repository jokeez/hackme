package hunt

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const (
	// LibFuzzerPublishTickItems is one mid-wall share. The claim wire still
	// accepts 96, and the on-disk cache holds 384. A tick stays smaller so one
	// shard cannot empty its corpus into the shared cache in a single post.
	LibFuzzerPublishTickItems = 32
	// LibFuzzerPublishTickBytes matches the claim wire byte cap. Together with
	// the item cap, one tick cannot carry the whole live directory.
	LibFuzzerPublishTickBytes = 256 << 10
)

type fileStamp struct {
	size int64
	mod  int64
}

// publishCursor is what this wall has already shared, plus the corpus that
// was on disk when the wall started. Names are stamps so an in-place rewrite
// is published once; hashes catch a peer unit that landed under a new name.
type publishCursor struct {
	mu     sync.Mutex
	files  map[string]fileStamp
	hashes map[string]struct{}
}

func newPublishCursor() *publishCursor {
	return &publishCursor{
		files:  map[string]fileStamp{},
		hashes: map[string]struct{}{},
	}
}

func (c *publishCursor) absorbNames(dir string) {
	if c == nil {
		return
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, ent := range ents {
		name := ent.Name()
		if !loadableSeedName(name) {
			continue
		}
		st, ok := isRegularCorpusFileUnder(dir, filepath.Join(dir, name))
		if !ok {
			continue
		}
		c.files[name] = fileStamp{size: st.Size(), mod: st.ModTime().UnixNano()}
	}
}

func (c *publishCursor) rememberBytes(units [][]byte) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, b := range units {
		if len(b) == 0 {
			continue
		}
		sum := sha256.Sum256(b)
		c.hashes[hex.EncodeToString(sum[:])] = struct{}{}
	}
}

func (c *publishCursor) rememberFiles(names []string, stamps []fileStamp) {
	if c == nil || len(names) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, name := range names {
		if name == "" || i >= len(stamps) {
			continue
		}
		c.files[name] = stamps[i]
	}
}

func (c *publishCursor) snapshot() (map[string]fileStamp, map[string]struct{}) {
	if c == nil {
		return nil, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	files := make(map[string]fileStamp, len(c.files))
	for k, v := range c.files {
		files[k] = v
	}
	hashes := make(map[string]struct{}, len(c.hashes))
	for k := range c.hashes {
		hashes[k] = struct{}{}
	}
	return files, hashes
}

type publishHit struct {
	name  string
	stamp fileStamp
	body  []byte
	sum   string
	skip  bool
}

// CollectLibFuzzerPublishUnits reads corpus units this wall has not shared yet.
// Crash artifacts, boot starters, symlinks, and anything that is not a regular
// file are left out. skipHashes are contents already shared or already on disk
// when the wall started. The result is newest-first and stops at the tick cap.
func CollectLibFuzzerPublishUnits(corpusDir string, skipHashes map[string]struct{}, maxItems, maxBytes int) [][]byte {
	units, _, _ := scanLibFuzzerPublish(corpusDir, nil, skipHashes, maxItems, maxBytes)
	return units
}

func scanLibFuzzerPublish(corpusDir string, skipNames map[string]fileStamp, skipHashes map[string]struct{}, maxItems, maxBytes int) (units [][]byte, names []string, stamps []fileStamp) {
	corpusDir = filepath.Clean(corpusDir)
	if corpusDir == "" || corpusDir == "." {
		return nil, nil, nil
	}
	if maxItems <= 0 {
		maxItems = LibFuzzerPublishTickItems
	}
	if maxBytes <= 0 {
		maxBytes = LibFuzzerPublishTickBytes
	}
	ents, err := os.ReadDir(corpusDir)
	if err != nil {
		return nil, nil, nil
	}
	hits := make([]publishHit, 0, len(ents))
	for _, ent := range ents {
		name := ent.Name()
		if !loadableSeedName(name) {
			continue
		}
		path := filepath.Join(corpusDir, name)
		st, ok := isRegularCorpusFileUnder(corpusDir, path)
		if !ok || st.Size() <= 0 || st.Size() > libFuzzerSeedMaxBytes {
			continue
		}
		stamp := fileStamp{size: st.Size(), mod: st.ModTime().UnixNano()}
		if prev, seen := skipNames[name]; seen && prev == stamp {
			continue
		}
		b, rerr := ReadRegularCorpusFileUnder(corpusDir, path, libFuzzerSeedMaxBytes)
		if rerr != nil || len(b) == 0 {
			continue
		}
		sum := sha256.Sum256(b)
		key := hex.EncodeToString(sum[:])
		hit := publishHit{name: name, stamp: stamp, sum: key}
		if genericBootSeed(b) {
			hit.skip = true
		} else if _, dup := skipHashes[key]; dup {
			hit.skip = true
		} else {
			hit.body = append([]byte(nil), b...)
		}
		hits = append(hits, hit)
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].stamp.mod == hits[j].stamp.mod {
			return hits[i].name < hits[j].name
		}
		return hits[i].stamp.mod > hits[j].stamp.mod
	})
	total := 0
	seenBatch := map[string]struct{}{}
	for _, hit := range hits {
		if hit.skip {
			names = append(names, hit.name)
			stamps = append(stamps, hit.stamp)
			continue
		}
		if len(units) >= maxItems {
			break
		}
		if _, dup := seenBatch[hit.sum]; dup {
			names = append(names, hit.name)
			stamps = append(stamps, hit.stamp)
			continue
		}
		if total+len(hit.body) > maxBytes {
			break
		}
		seenBatch[hit.sum] = struct{}{}
		units = append(units, hit.body)
		names = append(names, hit.name)
		stamps = append(stamps, hit.stamp)
		total += len(hit.body)
	}
	return units, names, stamps
}

// publishLibFuzzerSeeds posts units this wall grew. A failed post leaves them
// unseen so the next tick tries again. Files that were already in the corpus
// when the wall started are not part of the first post.
func publishLibFuzzerSeeds(ctx context.Context, corpusDir string, every time.Duration, known *publishCursor, publish func(context.Context, [][]byte) error) {
	if publish == nil || every <= 0 || known == nil {
		return
	}
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if ctx.Err() != nil {
				return
			}
			files, hashes := known.snapshot()
			units, names, stamps := scanLibFuzzerPublish(corpusDir, files, hashes, LibFuzzerPublishTickItems, LibFuzzerPublishTickBytes)
			if len(units) == 0 {
				if ctx.Err() != nil {
					return
				}
				known.rememberFiles(names, stamps)
				continue
			}
			if err := publish(ctx, units); err != nil {
				if ctx.Err() != nil {
					return
				}
				continue
			}
			if ctx.Err() != nil {
				return
			}
			known.rememberFiles(names, stamps)
			known.rememberBytes(units)
		}
	}
}

// libFuzzerShare is the pull and publish pair for one wall. stop marks the
// share closed, cancels the context, and waits. InstallEngineSeedFiles checks
// the flag, so it cannot start once stop has been asked, and a call already
// inside Install finishes before stop returns.
type libFuzzerShare struct {
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	once    sync.Once
	mu      sync.Mutex
	stopped bool
}

func (s *libFuzzerShare) stop() {
	if s == nil {
		return
	}
	s.once.Do(func() {
		s.mu.Lock()
		s.stopped = true
		s.mu.Unlock()
		if s.cancel != nil {
			s.cancel()
		}
		s.wg.Wait()
	})
}

func (s *libFuzzerShare) allowWrite() bool {
	if s == nil {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.stopped
}

func startLibFuzzerShare(parent context.Context, corpusDir string, every time.Duration, pull func(context.Context) ([][]byte, error), publish func(context.Context, [][]byte) error, known *publishCursor) *libFuzzerShare {
	ctx, cancel := context.WithCancel(parent)
	s := &libFuzzerShare{cancel: cancel}
	if pull != nil {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			pullLibFuzzerSeeds(ctx, corpusDir, every, pull, known, s)
		}()
	}
	if publish != nil && known != nil {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			publishLibFuzzerSeeds(ctx, corpusDir, every, known, publish)
		}()
	}
	return s
}
