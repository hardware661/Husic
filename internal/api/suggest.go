package api

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// Suggestions are deliberately independent of the complete, merged search.
// Only the two small search APIs race; YouTube never starts a process here.
func (s *Server) suggestions(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" || len(q) > 200 {
		fail(w, 400, "empty query")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 950*time.Millisecond)
	defer cancel()
	answers := make(chan []Item, 2)
	pending := 0
	for _, name := range []string{"netease", "qq"} {
		p := s.provider(name)
		if p == nil {
			continue
		}
		pending++
		go func() {
			tracks, err := p.Search(ctx, q, 6)
			items := []Item{}
			if err == nil {
				for _, t := range tracks[:min(6, len(tracks))] {
					if t.Title != "" {
						items = append(items, fromTrack(t))
					}
				}
			}
			select {
			case answers <- items:
			case <-ctx.Done():
			}
		}()
	}
	for ; pending > 0; pending-- {
		select {
		case items := <-answers:
			if len(items) > 0 {
				writeJSON(w, 200, map[string]any{"songs": items})
				return
			}
		case <-ctx.Done():
			pending = 0
		}
	}
	writeJSON(w, 200, map[string]any{"songs": []Item{}})
}
