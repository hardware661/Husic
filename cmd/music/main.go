// Command music is the Music server: it keeps the library, finds a playable
// copy of a song on whichever platform has one, and serves the web app.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"music/internal/accounts"
	"music/internal/api"
	"music/internal/netease"
	"music/internal/platform"
	"music/internal/resolve"
	"music/internal/source"
	"music/internal/store"
	"music/internal/youtube"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:18788", "address to serve on")
	dbFile := flag.String("db-file", "", "file holding the PostgreSQL URL (default: $MUSIC_DB)")
	sessionFile := flag.String("netease-session", "", "NetEase session file (optional)")
	accountState := flag.String("account-state", "", "private writable account connection directory")
	cnProxy := flag.String("cn-proxy", "", "proxy with a mainland exit for Chinese platforms, e.g. socks5h://127.0.0.1:18789")
	webDir := flag.String("web", "web/dist", "built web app")
	gatewayHeader := flag.String("gateway-header", "X-Music-Gateway", "header the edge proxy sets")
	gatewayKeyFile := flag.String("gateway-key-file", "", "file holding the edge proxy's proof (empty: loopback only)")
	ytBinary := flag.String("youtube-tool", "", "private yt-dlp binary (empty: YouTube disabled)")
	ytDeno := flag.String("youtube-deno", "", "private Deno binary")
	ytCookies := flag.String("youtube-cookies", "", "YouTube-only cookie import")
	ytState := flag.String("youtube-state", "", "private writable session/cache directory")
	flag.Parse()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(log, *listen, *dbFile, *sessionFile, *cnProxy, *webDir, *gatewayHeader, *gatewayKeyFile, *accountState, youtube.Config{Binary: *ytBinary, Deno: *ytDeno, Cookies: *ytCookies, StateDir: *ytState}); err != nil {
		log.Error("music stopped", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger, listen, dbFile, sessionFile, cnProxy, webDir, gatewayHeader, gatewayKeyFile, accountState string, ytConfig youtube.Config) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dsn := os.Getenv("MUSIC_DB")
	if dbFile != "" {
		dsn = readSecret(dbFile)
	}
	if dsn == "" {
		return errors.New("no database: set -db-file or MUSIC_DB")
	}
	db, err := store.Open(ctx, dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		return err
	}

	transport, err := source.NewHTTP(cnProxy, 12*time.Second)
	if err != nil {
		return err
	}
	server := &api.Server{Store: db, WebDir: webDir, Log: log, GatewayHeader: gatewayHeader}
	if gatewayKeyFile != "" {
		server.GatewayKey = readSecret(gatewayKeyFile)
	}
	var providers []source.Provider
	connections := &accounts.Manager{Dir: accountState, Log: log}
	seed := []byte("{}")
	if sessionFile != "" {
		seed = []byte(readSecret(sessionFile))
	}
	session, err := accounts.RestoreNetEase(seed, accountState)
	if err != nil {
		return errors.New("NetEase session file is not usable")
	}
	client, err := netease.New(cnProxy, &session, 12*time.Second)
	if err != nil {
		return err
	}
	server.NetEase, server.NetEaseUID = client, session.UserID
	connections.NetEase, connections.SeedHash = client, accounts.SeedHash(seed)
	providers = append(providers, source.NetEase{Client: client, Level: "exhigh"})
	// Priority after NetEase follows the coverage probe of 2026-10-22.
	qq, err := platform.New("qq", cnProxy)
	if err != nil {
		return err
	}
	bili, err := platform.New("bilibili", cnProxy)
	if err != nil {
		return err
	}
	kugou, err := platform.New("kugou", cnProxy)
	if err != nil {
		return err
	}
	migu, err := platform.New("migu", cnProxy)
	if err != nil {
		return err
	}
	// QQ/Kugou/Migu stay anonymous providers; their declined account flows
	// are neither restored nor registered. Existing files are left untouched.
	connections.Platforms = map[string]accounts.Platform{"bilibili": bili}
	server.Platforms = map[string]api.PlatformLibrary{"bilibili": bili}
	for _, kind := range []string{"bilibili"} {
		if err := connections.RestorePlatform(kind); err != nil {
			log.Warn("account restore unavailable", "platform", kind)
		}
	}
	providers = append(providers,
		qq, source.Kuwo{HTTP: transport}, kugou, migu)
	if ytConfig.Binary != "" {
		yt, err := youtube.New(ytConfig)
		if err != nil {
			return err
		}
		providers = append(providers, yt)
		server.YouTube = yt
		connections.YouTube = yt
	}
	providers = append(providers, bili)
	server.Accounts = connections
	server.Providers = providers
	server.Resolver = resolve.New(providers, db)

	httpServer := &http.Server{Addr: listen, Handler: server.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdown)
	}()
	go server.Warm(ctx)
	log.Info("music listening", "addr", listen, "netease", server.NetEase != nil, "providers", len(providers))
	if err := httpServer.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func readSecret(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
