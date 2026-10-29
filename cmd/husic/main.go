// Command husic is the Husic server: it keeps the library, finds a playable
// copy of a song on whichever platform has one, and serves the web app.
//
// Every flag can also be set with an environment variable named HUSIC_ plus
// the flag name in upper case, e.g. HUSIC_ACCOUNT_STATE for -account-state.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/hardware661/husic/internal/accounts"
	"github.com/hardware661/husic/internal/api"
	"github.com/hardware661/husic/internal/netease"
	"github.com/hardware661/husic/internal/platform"
	"github.com/hardware661/husic/internal/resolve"
	"github.com/hardware661/husic/internal/source"
	"github.com/hardware661/husic/internal/store"
	"github.com/hardware661/husic/internal/youtube"
)

func main() {
	listen := option("listen", "127.0.0.1:18788", "address to serve on")
	dbFile := option("db-file", "", "file holding the database URL (or set HUSIC_DB to the URL itself)")
	sessionFile := option("netease-session", "", "NetEase session file (optional)")
	accountState := option("account-state", "", "private writable directory for account connections (required)")
	cnProxy := option("cn-proxy", "", "proxy with a mainland exit for Chinese platforms, e.g. socks5h://127.0.0.1:18789")
	webDir := option("web", "web/dist", "built web app")
	gatewayHeader := option("gateway-header", "X-Husic-Gateway", "header the edge proxy sets")
	gatewayKeyFile := option("gateway-key-file", "", "file holding the edge proxy's proof (empty: loopback only)")
	ytBinary := option("youtube-tool", "", "absolute path of yt-dlp (empty: YouTube disabled)")
	ytDeno := option("youtube-deno", "", "absolute path of Deno, required with -youtube-tool")
	ytCookies := option("youtube-cookies", "", "YouTube-only cookie import")
	ytState := option("youtube-state", "", "private writable YouTube session/cache directory, required with -youtube-tool")
	flag.Parse()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(log, *listen, *dbFile, *sessionFile, *cnProxy, *webDir, *gatewayHeader, *gatewayKeyFile, *accountState, youtube.Config{Binary: *ytBinary, Deno: *ytDeno, Cookies: *ytCookies, StateDir: *ytState}); err != nil {
		log.Error("husic stopped", "err", err)
		os.Exit(1)
	}
}

// option is a string flag whose default comes from HUSIC_<NAME>.
func option(name, value, usage string) *string {
	env := "HUSIC_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
	if v, ok := os.LookupEnv(env); ok {
		value = v
	}
	return flag.String(name, value, usage+" ($"+env+")")
}

func run(log *slog.Logger, listen, dbFile, sessionFile, cnProxy, webDir, gatewayHeader, gatewayKeyFile, accountState string, ytConfig youtube.Config) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dsn := os.Getenv("HUSIC_DB")
	if dbFile != "" {
		var err error
		if dsn, err = readSecret("-db-file", dbFile); err != nil {
			return err
		}
	}
	if dsn == "" {
		return errors.New("no database: set HUSIC_DB to postgres://… or sqlite:/path/to/husic.db, or -db-file to a file holding one")
	}
	if err := privateDir("-account-state", accountState); err != nil {
		return err
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
		if server.GatewayKey, err = readSecret("-gateway-key-file", gatewayKeyFile); err != nil {
			return err
		}
	}
	var providers []source.Provider
	connections := &accounts.Manager{Dir: accountState, Log: log}
	seed := []byte("{}")
	if sessionFile != "" {
		text, err := readSecret("-netease-session", sessionFile)
		if err != nil {
			return err
		}
		seed = []byte(text)
	}
	session, err := accounts.RestoreNetEase(seed, accountState)
	if err != nil {
		return fmt.Errorf("-netease-session %s: not a NetEase session JSON file", sessionFile)
	}
	client, err := netease.New(cnProxy, &session, 12*time.Second)
	if err != nil {
		return err
	}
	server.NetEase = client
	connections.NetEase, connections.SeedHash = client, accounts.SeedHash(seed)
	providers = append(providers, source.NetEase{Client: client, Level: "exhigh"})
	// After NetEase, providers are tried in order of how often they have a
	// full-length copy. QQ Music, Kuwo, Kugou and Migu are used anonymously.
	bili, err := platform.New(cnProxy)
	if err != nil {
		return err
	}
	connections.Platforms = map[string]accounts.Platform{"bilibili": bili}
	server.Platforms = map[string]api.PlatformLibrary{"bilibili": bili}
	if err := connections.RestorePlatform("bilibili"); err != nil {
		log.Warn("account restore unavailable", "platform", "bilibili")
	}
	providers = append(providers,
		source.QQ{HTTP: transport}, source.Kuwo{HTTP: transport}, source.Kugou{HTTP: transport},
		source.Migu{HTTP: transport})
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
	log.Info("husic listening", "addr", listen, "netease", server.NetEase != nil, "providers", len(providers))
	if err := httpServer.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// readSecret reads a one-value file; a flag that names a file must work.
func readSecret(flagName, path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("%s: %w", flagName, err)
	}
	value := strings.TrimSpace(string(data))
	if value == "" {
		return "", fmt.Errorf("%s %s: file is empty", flagName, path)
	}
	return value, nil
}

// privateDir makes sure account connections can be saved before anyone
// scans a QR code, instead of failing after the scan.
func privateDir(flagName, path string) error {
	if path == "" {
		return fmt.Errorf("%s is required: a private directory where account connections are saved", flagName)
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("%s: %w", flagName, err)
	}
	probe, err := os.CreateTemp(path, ".write-check-*")
	if err != nil {
		return fmt.Errorf("%s %s: not writable: %w", flagName, path, err)
	}
	probe.Close()
	return os.Remove(probe.Name())
}
