// Command cubicle-gateway is the Cubicle control plane: it serves the API, keeps
// the office state in SQLite, fires routines, and supervises the agent worker.
//
//	cubicle-gateway [serve]                run in the foreground (default)
//	cubicle-gateway service install|uninstall|start|stop
//	cubicle-gateway version
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/api"
	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/config"
	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/events"
	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/secrets"
	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/store"
	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/supervisor"
	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/version"
	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/winsvc"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "cubicle-gateway:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cmd := "serve"
	if len(args) > 0 {
		cmd = args[0]
	}
	switch cmd {
	case "serve":
		if winsvc.IsService() {
			return winsvc.RunService(func(ctx context.Context) error { return serve(ctx, true) })
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return serve(ctx, false)
	case "service":
		if len(args) < 2 {
			return errors.New("usage: cubicle-gateway service install|uninstall|start|stop")
		}
		switch args[1] {
		case "install":
			return winsvc.Install()
		case "uninstall":
			return winsvc.Uninstall()
		case "start":
			return winsvc.Start()
		case "stop":
			return winsvc.Stop()
		}
		return fmt.Errorf("unknown service command %q", args[1])
	case "version", "--version", "-v":
		fmt.Printf("cubicle-gateway %s (%s)\n", version.Version, version.Commit)
		return nil
	case "help", "--help", "-h":
		fmt.Println("usage: cubicle-gateway [serve | service install|uninstall|start|stop | version]")
		return nil
	}
	return fmt.Errorf("unknown command %q", cmd)
}

func serve(ctx context.Context, serviceMode bool) error {
	cfg, err := config.Load(serviceMode)
	if err != nil {
		return err
	}
	log := newLogger(cfg.Home)
	log.Info("starting cubicle-gateway", "version", version.Version, "addr", cfg.Addr, "home", cfg.Home, "service", serviceMode)

	st, err := store.Open(filepath.Join(cfg.Home, "cubicle.db"))
	if err != nil {
		return err
	}
	defer st.Close()
	box, err := secrets.Open(cfg.Home)
	if err != nil {
		return err
	}

	hub := events.NewHub()
	srv := api.New(cfg, st, hub, box, log)
	go srv.RunScheduler(ctx)
	go srv.RunBreakerClock(ctx)

	if !cfg.DisableWorker {
		command, wargs := cfg.WorkerCommand, cfg.WorkerArgs
		if command == "" {
			command = supervisor.Discover()
		}
		if command == "" {
			log.Warn("no agent worker found; start it yourself with `uv run cubicle-agents` (see README)")
		} else {
			scheme := "http"
			if cfg.TLSEnabled() {
				scheme = "https"
			}
			gatewayURL := scheme + "://" + loopbackAddr(cfg.Addr)
			sup := supervisor.New(command, wargs, func() []string {
				return append(srv.WorkerEnv(),
					"CUBICLE_GATEWAY_URL="+gatewayURL,
					"CUBICLE_WORKER_TOKEN="+cfg.WorkerToken,
					"CUBICLE_HOME="+cfg.Home,
				)
			}, log)
			sup.OnExit = func(error) { _ = st.MarkAllOffline(context.Background()); hub.Publish("employees.offline", nil) }
			srv.OnConfigChanged = sup.Restart
			srv.RunWorker = func(ctx context.Context, args, env []string, out any) error {
				return runWorkerJSON(ctx, command, wargs, args, env, out)
			}
			go sup.Run(ctx)
		}
	}

	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    32 << 10,
		TLSConfig:         &tls.Config{MinVersion: tls.VersionTLS12},
	}
	errCh := make(chan error, 1)
	go func() {
		if cfg.TLSEnabled() {
			errCh <- httpSrv.ListenAndServeTLS(cfg.TLSCert, cfg.TLSKey)
		} else {
			errCh <- httpSrv.ListenAndServe()
		}
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Info("shutting down")
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutCtx)
	}
	_ = st.MarkAllOffline(context.Background())
	return nil
}

// loopbackAddr rewrites wildcard listen addresses so the worker connects locally.
func loopbackAddr(addr string) string {
	if strings.HasPrefix(addr, "0.0.0.0:") || strings.HasPrefix(addr, ":") {
		return "127.0.0.1:" + addr[strings.LastIndex(addr, ":")+1:]
	}
	if strings.HasPrefix(addr, "[::]:") {
		return "[::1]:" + addr[strings.LastIndex(addr, ":")+1:]
	}
	return addr
}

func newLogger(home string) *slog.Logger {
	var w io.Writer = os.Stderr
	logDir := filepath.Join(home, "logs")
	if err := os.MkdirAll(logDir, 0o700); err == nil {
		if f, err := os.OpenFile(filepath.Join(logDir, "gateway.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
			w = io.MultiWriter(os.Stderr, f)
		}
	}
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelInfo}))
}
