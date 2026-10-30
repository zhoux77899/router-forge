package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/gofrs/flock"
	"github.com/zhoux77899/router-forge/internal/control"
	"github.com/zhoux77899/router-forge/internal/domain"
	"github.com/zhoux77899/router-forge/internal/executor"
	"github.com/zhoux77899/router-forge/internal/gateway"
	backendruntime "github.com/zhoux77899/router-forge/internal/runtime"
	"github.com/zhoux77899/router-forge/internal/store"
)

func run() error {
	defaults, err := backendruntime.DefaultDataDir()
	if err != nil {
		return err
	}
	data := flag.String("data-dir", defaults, "Private application data directory")
	listen := flag.String("listen", "127.0.0.1:8787", "Loopback-only control/results listener")
	workers := flag.String("workers-dir", "", "Bundled native/Linux worker resources")
	web := flag.String("web-dir", "", "Built read-only Vite application")
	origin := flag.String("dev-origin", "", "Explicit loopback development origin")
	version := flag.Bool("version", false, "Print application version")
	flag.Parse()
	if *version {
		fmt.Println(domain.Version)
		return nil
	}
	host, _, err := net.SplitHostPort(*listen)
	if err != nil {
		return err
	}
	if host != "127.0.0.1" && host != "::1" {
		return fmt.Errorf("backend listen address must be literal loopback")
	}
	abs, err := filepath.Abs(*data)
	if err != nil {
		return err
	}
	if err = store.EnsurePrivateDir(abs); err != nil {
		return err
	}
	lockPath := filepath.Join(abs, "server.lock")
	if err = store.EnsurePrivateFile(lockPath); err != nil {
		return err
	}
	lock := flock.New(lockPath)
	locked, err := lock.TryLock()
	if err != nil {
		return err
	}
	if !locked {
		return fmt.Errorf("this application profile is already owned by another backend")
	}
	defer lock.Unlock()
	db, err := store.Open(abs)
	if err != nil {
		return err
	}
	defer db.Close()
	if err = db.RecoverInterrupted(context.Background()); err != nil {
		return err
	}
	if *workers == "" {
		cwd, _ := os.Getwd()
		candidate := filepath.Join(cwd, "build", "workers")
		if _, err = os.Stat(candidate); err == nil {
			*workers = candidate
		} else {
			exe, _ := os.Executable()
			*workers = filepath.Join(filepath.Dir(exe), "..", "workers")
		}
	}
	if *web == "" {
		cwd, _ := os.Getwd()
		candidate := filepath.Join(cwd, "build", "dist")
		if _, err = os.Stat(candidate); err == nil {
			*web = candidate
		}
	}
	if *workers != "" {
		*workers, err = filepath.Abs(*workers)
		if err != nil {
			return err
		}
	}
	if *web != "" {
		*web, err = filepath.Abs(*web)
		if err != nil {
			return err
		}
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	gw := gateway.New(db)
	exec := executor.New(*workers, gw)
	token := domain.NewID("control") + domain.NewID("nonce")
	app, err := control.New(ctx, db, gw, exec, control.Config{ControlToken: token, DevOrigin: *origin, WebDir: *web})
	if err != nil {
		return err
	}
	defer app.Close()
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		return err
	}
	info := domain.BackendInfo{URL: "http://" + listener.Addr().String(), ControlToken: token, PID: os.Getpid(), Version: domain.Version, DataDir: abs, WorkerDir: *workers}
	b, _ := json.MarshalIndent(info, "", "  ")
	if err = store.WritePrivateFile(filepath.Join(abs, "backend.json"), append(b, '\n')); err != nil {
		listener.Close()
		return err
	}
	server := &http.Server{Handler: app, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 64 << 10}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	fmt.Printf("Router Forge %s ready at %s (PID %d); credentials remain in the private application directory.\n", domain.Version, info.URL, info.PID)
	select {
	case err := <-done:
		if err != http.ErrServerClosed {
			return err
		}
		return nil
	case <-ctx.Done():
		app.Close()
		shutdown, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		return server.Shutdown(shutdown)
	}
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Router Forge backend:", err)
		os.Exit(1)
	}
}
