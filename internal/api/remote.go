package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
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
	state  remoteState
}

// remoteState is what a player last said it is doing.
type remoteState struct {
	Open    bool   `json:"open"`
	Title   string `json:"title,omitempty"`
	Artist  string `json:"artist,omitempty"`
	Playing bool   `json:"playing,omitempty"`
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
		p.active, p.state = old.active, old.state // a reconnect keeps its place
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

func (h *remoteHub) report(id string, st remoteState) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if p := h.players[id]; p != nil {
		p.state = st
	}
}

// current is the player commands go to: the desktop app on the owner's PC
// when one is open (the speaker's commands are for the PC; speaker v6 §2.6:
// a phone tab used later took them), else the most recently used player.
// Call with h.mu held.
func (h *remoteHub) current() *remotePlayer {
	var best *remotePlayer
	for _, p := range h.players {
		if best == nil || isDesktop(p) && !isDesktop(best) || isDesktop(p) == isDesktop(best) && p.active.After(best.active) {
			best = p
		}
	}
	return best
}

// isDesktop: the Husic desktop app (its player id starts with "exe-").
func isDesktop(p *remotePlayer) bool { return strings.HasPrefix(p.id, "exe-") }

// stateNow is what the player commands go to is doing.
func (h *remoteHub) stateNow() remoteState {
	h.mu.Lock()
	defer h.mu.Unlock()
	p := h.current()
	if p == nil {
		return remoteState{}
	}
	st := p.state
	st.Open = true
	return st
}

// send delivers c to the most recently used player; false if none is open.
func (h *remoteHub) send(c remoteCommand) (string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	best := h.current()
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

// remoteReport: a player says what it plays (on every song change and
// play/pause), so the speaker's panel can show it.
func (s *Server) remoteReport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Player  string `json:"player"`
		Title   string `json:"title"`
		Artist  string `json:"artist"`
		Playing bool   `json:"playing"`
	}
	if err := readBody(r, &req); err != nil || !validPlayerID(req.Player) || len(req.Title) > 300 || len(req.Artist) > 300 {
		fail(w, http.StatusBadRequest, "bad request")
		return
	}
	s.remote.report(req.Player, remoteState{Title: req.Title, Artist: req.Artist, Playing: req.Playing})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) remoteStateNow(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.remote.stateNow())
}
