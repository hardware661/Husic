package api

import (
	"context"
	"net/http"
	"strconv"

	"music/internal/platform"
)

type PlatformLibrary interface {
	Collections(context.Context) ([]platform.Collection, error)
	Collection(context.Context, string, int) (platform.CollectionPage, error)
	Video(context.Context, string) (platform.Video, error)
	http.Handler
}

func (s *Server) platformRoutes(mux *http.ServeMux) {
	for _, kind := range []string{"bilibili"} {
		mux.HandleFunc("GET /api/accounts/"+kind+"/collections", func(w http.ResponseWriter, r *http.Request) {
			client := s.Platforms[kind]
			if client == nil {
				fail(w, 503, "账号连接尚未配置")
				return
			}
			collections, err := client.Collections(r.Context())
			if err != nil {
				fail(w, 502, err.Error())
				return
			}
			writeJSON(w, 200, map[string]any{"collections": collections})
		})
		mux.HandleFunc("GET /api/accounts/"+kind+"/collections/{id}", func(w http.ResponseWriter, r *http.Request) {
			client := s.Platforms[kind]
			if client == nil {
				fail(w, 503, "账号连接尚未配置")
				return
			}
			page := 1
			if value := r.URL.Query().Get("page"); value != "" {
				page, _ = strconv.Atoi(value)
			}
			result, err := client.Collection(r.Context(), r.PathValue("id"), page)
			if err != nil {
				fail(w, 502, err.Error())
				return
			}
			items := make([]Item, 0, len(result.Tracks))
			for _, t := range result.Tracks {
				items = append(items, fromTrack(t))
			}
			s.annotate(r.Context(), items)
			writeJSON(w, 200, map[string]any{"collection": result.Collection, "songs": items, "total": result.Total, "next": result.Next})
		})
	}
	mux.HandleFunc("GET /api/bilibili/videos/{id}", func(w http.ResponseWriter, r *http.Request) {
		client := s.Platforms["bilibili"]
		if client == nil {
			fail(w, 503, "连接尚未配置")
			return
		}
		video, err := client.Video(r.Context(), r.PathValue("id"))
		if err != nil {
			fail(w, 502, err.Error())
			return
		}
		items := make([]Item, 0, len(video.Parts))
		for _, t := range video.Parts {
			items = append(items, fromTrack(t))
		}
		s.annotate(r.Context(), items)
		writeJSON(w, 200, map[string]any{"video": map[string]any{"id": video.ID, "title": video.Title, "cover": video.Cover}, "songs": items})
	})
	if client := s.Platforms["bilibili"]; client != nil {
		mux.Handle("GET /api/bilibili/media/{token}", client)
	}
}
