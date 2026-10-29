package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/skip2/go-qrcode"
	"music/internal/accounts"
)

// These routes remain behind the existing gateway. A cross-site page cannot
// start, confirm, or replace a connection even when the browser is signed in.
func (s *Server) accountRoutes(mux *http.ServeMux) {
	mutate := func(handler http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if s.Accounts == nil {
				fail(w, 503, "账号连接暂不可用")
				return
			}
			origin := r.Header.Get("Origin")
			if origin != "" {
				parsed, err := url.Parse(origin)
				scheme := "http"
				if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
					scheme = "https"
				}
				if err != nil || parsed.Host != r.Host || parsed.Scheme != scheme {
					fail(w, 403, "请从 Music 设置中操作")
					return
				}
			}
			if r.Header.Get("Sec-Fetch-Site") == "cross-site" || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
				fail(w, 403, "请从 Music 设置中操作")
				return
			}
			handler(w, r)
		}
	}
	mux.HandleFunc("GET /api/accounts", func(w http.ResponseWriter, r *http.Request) {
		if s.Accounts == nil {
			fail(w, 503, "账号连接暂不可用")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
		defer cancel()
		writeJSON(w, 200, s.Accounts.Status(ctx))
	})
	for _, kind := range []string{"bilibili"} {
		mux.HandleFunc("POST /api/accounts/"+kind+"/login", mutate(func(w http.ResponseWriter, r *http.Request) {
			attempt, err := s.Accounts.StartPlatform(r.Context(), kind)
			if err != nil {
				fail(w, 502, err.Error())
				return
			}
			writeJSON(w, 200, attempt)
		}))
		mux.HandleFunc("GET /api/accounts/"+kind+"/login/{id}/qr", func(w http.ResponseWriter, r *http.Request) {
			if s.Accounts == nil {
				fail(w, 503, "账号连接暂不可用")
				return
			}
			png, err := s.Accounts.PlatformQR(kind, r.PathValue("id"))
			if err != nil {
				fail(w, 410, err.Error())
				return
			}
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			_, _ = w.Write(png)
		})
		mux.HandleFunc("POST /api/accounts/"+kind+"/login/{id}/check", mutate(func(w http.ResponseWriter, r *http.Request) {
			attempt, err := s.Accounts.CheckPlatform(r.Context(), kind, r.PathValue("id"))
			if err != nil {
				if errors.Is(err, accounts.ErrAttemptGone) {
					fail(w, 410, err.Error())
					return
				}
				fail(w, 502, err.Error())
				return
			}
			if attempt.State == "connected" {
				s.cacheMu.Lock()
				for key := range s.cache {
					if strings.HasPrefix(key, "recommendation:"+kind+":") {
						delete(s.cache, key)
					}
				}
				s.cacheMu.Unlock()
			}
			writeJSON(w, 200, attempt)
		}))
	}
	mux.HandleFunc("POST /api/accounts/netease/login", mutate(func(w http.ResponseWriter, r *http.Request) {
		attempt, err := s.Accounts.StartNetEase(r.Context())
		if err != nil {
			fail(w, 502, err.Error())
			return
		}
		writeJSON(w, 200, attempt)
	}))
	mux.HandleFunc("GET /api/accounts/netease/login/{id}/qr", func(w http.ResponseWriter, r *http.Request) {
		if s.Accounts == nil {
			fail(w, 503, "账号连接暂不可用")
			return
		}
		link, err := s.Accounts.NetEaseQR(r.PathValue("id"))
		if err != nil {
			fail(w, 410, err.Error())
			return
		}
		png, err := qrcode.Encode(link, qrcode.Medium, 320)
		if err != nil {
			fail(w, 500, "二维码生成失败")
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write(png)
	})
	mux.HandleFunc("POST /api/accounts/netease/login/{id}/check", mutate(func(w http.ResponseWriter, r *http.Request) {
		attempt, err := s.Accounts.CheckNetEase(r.Context(), r.PathValue("id"))
		if err != nil {
			fail(w, 502, err.Error())
			return
		}
		if attempt.State == "connected" {
			s.cacheMu.Lock()
			s.cache = nil
			s.cacheMu.Unlock()
		}
		writeJSON(w, 200, attempt)
	}))
	mux.HandleFunc("POST /api/accounts/youtube/login", mutate(func(w http.ResponseWriter, r *http.Request) {
		attempt, err := s.Accounts.StartYouTube()
		if err != nil {
			fail(w, 503, err.Error())
			return
		}
		writeJSON(w, 200, attempt)
	}))
	mux.HandleFunc("POST /api/accounts/youtube/login/{id}/complete", mutate(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Cookies string `json:"cookies"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 600<<10)
		if json.NewDecoder(r.Body).Decode(&payload) != nil {
			fail(w, 400, "登录信息不完整，请重新连接")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
		defer cancel()
		attempt, err := s.Accounts.CompleteYouTube(ctx, r.PathValue("id"), []byte(payload.Cookies))
		if err != nil {
			fail(w, 400, err.Error())
			return
		}
		writeJSON(w, 200, attempt)
	}))
	mux.HandleFunc("POST /api/accounts/youtube/check", mutate(func(w http.ResponseWriter, r *http.Request) {
		if s.Accounts.YouTube == nil {
			fail(w, 503, "YouTube 尚未配置")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
		defer cancel()
		err := s.Accounts.YouTube.Verify(ctx)
		writeJSON(w, 200, map[string]any{"ok": err == nil, "youtube": s.Accounts.YouTube.Details()})
	}))
	mux.HandleFunc("DELETE /api/accounts/attempts/{id}", mutate(func(w http.ResponseWriter, r *http.Request) {
		s.Accounts.Cancel(r.PathValue("id"))
		writeJSON(w, 200, map[string]bool{"ok": true})
	}))
}
