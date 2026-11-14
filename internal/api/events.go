package api

import (
	"net/http"
	"time"
)

// playbackEvent is one play/pause the web player saw, with where it came
// from (a page button, the OS media session, the desktop shell, the player
// resuming by itself). Logged for diagnosing who starts or stops playback —
// e.g. a Bluetooth speaker sending pause/play — never stored.
type playbackEvent struct {
	At     int64  `json:"at"` // ms since epoch, browser clock
	Action string `json:"action"`
	From   string `json:"from"`
	State  string `json:"state"` // player state before the event
}

func (s *Server) playbackEvents(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Events []playbackEvent `json:"events"`
	}
	if err := readBody(r, &req); err != nil || len(req.Events) > 50 {
		fail(w, http.StatusBadRequest, "bad request")
		return
	}
	for _, e := range req.Events {
		if len(e.Action) > 32 || len(e.From) > 32 || len(e.State) > 32 {
			fail(w, http.StatusBadRequest, "bad request")
			return
		}
	}
	if s.Log != nil {
		for _, e := range req.Events {
			s.Log.Info("playback", "at", time.UnixMilli(e.At).Format("15:04:05.000"), "action", e.Action, "from", e.From, "state", e.State)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
