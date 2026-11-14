package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Remote control: another program on the same host (a smart-speaker bridge,
// with the service key) asks the owner's open Husic — the desktop app or a
// browser tab — to play, skip, pause or stop. Each open player keeps
// GET /api/remote/listen (Server-Sent Events) and says when the owner last
// used it (POST /api/remote/active); a command goes to the most recently
// used one only, so two players never start together.

// remoteCommand is what a player is told to do.
type remoteCommand struct {
	Action string `json:"action"`         // play | next | pause | resume | stop
	What   string `json:"what,omitempty"` // play: "liked" or "playlist:<name>"
}

var remoteActions = map[string]bool{"play": true, "next": true, "pause": true, "resume": true, "stop": true}

type remotePlayer struct {
	id     string
	out    chan remoteCommand
	active time.Time
}

type remoteHub struct {
	mu      sync.Mutex
	players map[string]*remotePlayer
}

func (h *remoteHub) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.players)
}

func (h *remoteHub) join(id string) *remotePlayer {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.players == nil {
		h.players = map[string]*remotePlayer{}
	}
	p := &remotePlayer{id: id, out: make(chan remoteCommand, 4)}
	if old := h.players[id]; old != nil {
		p.active = old.active // a reconnect keeps its place
	}
	h.players[id] = p
	return p
}

func (h *remoteHub) leave(p *remotePlayer) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.players[p.id] == p {
		delete(h.players, p.id)
	}
}

func (h *remoteHub) touch(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if p := h.players[id]; p != nil {
		p.active = time.Now()
	}
}

// send delivers c to the most recently used player; false if none is open.
func (h *remoteHub) send(c remoteCommand) (string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	var best *remotePlayer
	for _, p := range h.players {
		if best == nil || p.active.After(best.active) {
			best = p
		}
	}
	if best == nil {
		return "", false
	}
	select {
	case best.out <- c:
	default: // a stuck player: drop rather than block the caller
	}
	return best.id, true
}

func validPlayerID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if !(r == '-' || r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return false
		}
	}
	return true
}

func (s *Server) remoteListen(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("player")
	if !validPlayerID(id) {
		fail(w, http.StatusBadRequest, "bad player")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		fail(w, http.StatusInternalServerError, "no streaming")
		return
	}
	p := s.remote.join(id)
	defer s.remote.leave(p)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()
	ping := time.NewTicker(25 * time.Second) // keeps proxies from closing an idle stream
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
		case c := <-p.out:
			b, _ := json.Marshal(c)
			fmt.Fprintf(w, "data: %s\n\n", b)
		}
		flusher.Flush()
	}
}

func (s *Server) remoteActive(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Player string `json:"player"`
	}
	if err := readBody(r, &req); err != nil || !validPlayerID(req.Player) {
		fail(w, http.StatusBadRequest, "bad request")
		return
	}
	s.remote.touch(req.Player)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) remoteCommand(w http.ResponseWriter, r *http.Request) {
	var c remoteCommand
	if err := readBody(r, &c); err != nil || !remoteActions[c.Action] || len(c.What) > 200 {
		fail(w, http.StatusBadRequest, "bad command")
		return
	}
	id, ok := s.remote.send(c)
	if !ok {
		fail(w, http.StatusConflict, "no player open")
		return
	}
	if s.Log != nil {
		s.Log.Info("remote command", "action", c.Action, "player", id)
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"player": id})
}
