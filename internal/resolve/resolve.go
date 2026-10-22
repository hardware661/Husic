// Package resolve turns a song into a playable stream: it tries the known
// platform copies in priority order and, when they all fail, looks for the
// same recording on the other platforms.
package resolve

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"

	"music/internal/source"
)

// Linker remembers copies found on the way; the store implements it.
type Linker interface {
	LinkTrack(ctx context.Context, songID int64, t source.Track) error
	MarkSource(ctx context.Context, src, id string, ok bool, reason string) error
}

type Resolver struct {
	providers map[string]source.Provider
	rank      map[string]int
	order     []string
	links     Linker

	mu    sync.Mutex
	cache map[string]cached
}

type cached struct {
	stream source.Stream
	until  time.Time
}

// Signed addresses last 20 minutes on the shortest-lived platform.
const cacheFor = 10 * time.Minute

// New tries providers in the order given. links may be nil.
func New(providers []source.Provider, links Linker) *Resolver {
	r := &Resolver{providers: map[string]source.Provider{}, rank: map[string]int{}, links: links, cache: map[string]cached{}}
	for i, p := range providers {
		r.providers[p.Name()] = p
		r.rank[p.Name()] = i
		r.order = append(r.order, p.Name())
	}
	return r
}

// Order is the provider priority, best first.
func (r *Resolver) Order() []string { return slices.Clone(r.order) }

// Resolve returns the copy that played and its stream. songID 0 means the
// song is not in the library, so new copies are not recorded.
func (r *Resolver) Resolve(ctx context.Context, songID int64, want source.Track, copies []source.Track, rejected ...string) (source.Track, source.Stream, error) {
	blocked := make(map[string]bool, len(rejected))
	for _, ref := range rejected {
		blocked[ref] = true
		r.Forget(ref)
	}
	copies = slices.Clone(copies)
	slices.SortStableFunc(copies, func(a, b source.Track) int {
		if a.Pinned != b.Pinned {
			if a.Pinned {
				return -1
			}
			return 1
		}
		return cmp.Compare(r.priority(a.Source), r.priority(b.Source))
	})
	tried := map[string]bool{}
	for _, t := range copies {
		if blocked[t.Ref()] {
			continue
		}
		tried[t.Source] = true
		if stream, err := r.stream(ctx, t); err == nil {
			return t, stream, nil
		}
	}
	for _, t := range r.findElsewhere(ctx, want, tried) {
		if blocked[t.Ref()] {
			continue
		}
		if stream, err := r.stream(ctx, t); err == nil {
			if songID != 0 && r.links != nil {
				_ = r.links.LinkTrack(context.WithoutCancel(ctx), songID, t)
			}
			return t, stream, nil
		}
	}
	return source.Track{}, source.Stream{}, source.ErrUnavailable
}

func (r *Resolver) priority(name string) int {
	if n, ok := r.rank[name]; ok {
		return n
	}
	return len(r.rank)
}

func (r *Resolver) stream(ctx context.Context, t source.Track) (source.Stream, error) {
	p := r.providers[t.Source]
	if p == nil {
		return source.Stream{}, source.ErrUnavailable
	}
	key := t.Ref()
	r.mu.Lock()
	hit, ok := r.cache[key]
	r.mu.Unlock()
	if ok && time.Now().Before(hit.until) {
		return hit.stream, nil
	}
	budget := 6 * time.Second
	if slow, ok := p.(interface{ Budget() time.Duration }); ok {
		budget = slow.Budget()
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	stream, err := p.Stream(ctx, t)
	if err == nil && t.Duration > 0 && stream.Seconds > 0 && stream.Seconds < t.Duration-6 {
		err = source.ErrUnavailable // a preview clip, not the song
	}
	if r.links != nil {
		reason := ""
		if err != nil {
			reason = err.Error()
		}
		_ = r.links.MarkSource(context.WithoutCancel(ctx), t.Source, t.ID, err == nil, reason)
	}
	if err != nil {
		return source.Stream{}, err
	}
	r.mu.Lock()
	r.cache[key] = cached{stream: stream, until: time.Now().Add(cacheFor)}
	r.mu.Unlock()
	return stream, nil
}

// findElsewhere searches the platforms not tried yet, all at once, and
// returns their copies of the same recording in priority order.
func (r *Resolver) findElsewhere(ctx context.Context, want source.Track, tried map[string]bool) []source.Track {
	if want.Title == "" {
		return nil
	}
	query := want.Title
	if len(want.Artists) > 0 {
		query += " " + want.Artists[0]
	}
	var mu sync.Mutex
	var found []source.Track
	var wg sync.WaitGroup
	for name, p := range r.providers {
		if tried[name] {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
			defer cancel()
			tracks, err := p.Search(ctx, query, 8)
			if err != nil {
				return
			}
			for _, t := range tracks {
				if source.SameSong(want, t) {
					mu.Lock()
					found = append(found, t)
					mu.Unlock()
					return
				}
			}
		}()
	}
	wg.Wait()
	slices.SortStableFunc(found, func(a, b source.Track) int { return cmp.Compare(r.priority(a.Source), r.priority(b.Source)) })
	return found
}

// Forget drops cached streams of a track, e.g. after the player hit an error.
func (r *Resolver) Forget(ref string) {
	r.mu.Lock()
	delete(r.cache, ref)
	r.mu.Unlock()
	if src, id, ok := strings.Cut(ref, ":"); ok {
		if provider, ok := r.providers[src].(interface{ Forget(string) }); ok {
			provider.Forget(id)
		}
	}
}

// IsUnavailable reports whether err means "no platform will play it".
func IsUnavailable(err error) bool {
	return errors.Is(err, source.ErrUnavailable) || err != nil && strings.Contains(err.Error(), "unavailable")
}
