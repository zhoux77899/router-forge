package control

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zhoux77899/router-forge/internal/domain"
	"github.com/zhoux77899/router-forge/internal/gateway"
	"github.com/zhoux77899/router-forge/internal/store"
)

type Executor interface {
	Inspect(context.Context, domain.Environment) (domain.Inspection, error)
	Resolve(context.Context, domain.Environment, domain.Suite) (domain.ResolvedBenchmark, error)
	Tasks(context.Context, domain.Environment, domain.Suite) ([]domain.BenchmarkTask, error)
	Execute(context.Context, domain.Environment, domain.ExecutionSpec, func(domain.WorkerEvent), func(domain.ExecutionObservation) error) (domain.ExecutionResult, error)
	Close() error
}

type Config struct {
	ControlToken string
	DevOrigin    string
	WebDir       string
}
type activeRun struct {
	cancel context.CancelFunc
	token  string
}
type Server struct {
	store    *store.Store
	gateway  *gateway.Gateway
	executor Executor
	ctx      context.Context
	config   Config
	mux      *http.ServeMux
	mu       sync.Mutex
	wg       sync.WaitGroup
	closed   bool
	active   map[string]*activeRun
}

func New(ctx context.Context, s *store.Store, g *gateway.Gateway, e Executor, config Config) (*Server, error) {
	if len(config.ControlToken) < 32 {
		return nil, fmt.Errorf("a strong local control token is required")
	}
	if config.DevOrigin != "" {
		u, err := url.Parse(config.DevOrigin)
		if err != nil || u.Scheme != "http" || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") || u.Path != "" {
			return nil, fmt.Errorf("development origin must be loopback HTTP")
		}
	}
	server := &Server{store: s, gateway: g, executor: e, ctx: ctx, config: config, mux: http.NewServeMux(), active: map[string]*activeRun{}}
	server.routes()
	return server, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		host = parsed
	}
	host = strings.Trim(host, "[]")
	if host != "localhost" && host != "127.0.0.1" && host != "::1" {
		fail(w, http.StatusForbidden, "loopback host required")
		return
	}
	origin := r.Header.Get("Origin")
	if origin != "" && origin != "http://"+r.Host && origin != s.config.DevOrigin {
		fail(w, http.StatusForbidden, "untrusted browser origin")
		return
	}
	if origin != "" {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	s.mux.ServeHTTP(w, r)
}

func (s *Server) admin(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(provided), []byte(s.config.ControlToken)) != 1 {
			fail(w, http.StatusUnauthorized, "Desktop control authentication required")
			return
		}
		if r.Method != "GET" && !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			fail(w, http.StatusUnsupportedMediaType, "application/json required")
			return
		}
		handler(w, r)
	}
}

func write(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(value)
}
func fail(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
func problem(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	if errors.Is(err, domain.ErrNotFound) {
		status = http.StatusNotFound
	}
	if errors.Is(err, domain.ErrConflict) {
		status = http.StatusConflict
	}
	fail(w, status, err.Error())
}
func decode[T any](w http.ResponseWriter, r *http.Request) (T, bool) {
	var value T
	reader := http.MaxBytesReader(w, r.Body, 2<<20)
	defer reader.Close()
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		fail(w, 400, "invalid JSON request")
		return value, false
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		fail(w, 400, "one JSON object required")
		return value, false
	}
	return value, true
}
func pagination(r *http.Request) (int, int) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit < 1 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		write(w, map[string]any{"version": domain.Version, "pid": os.Getpid(), "running": true})
	})
	s.mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) { write(w, map[string]string{"status": "ok"}) })
	s.mux.HandleFunc("GET /api/environments", s.admin(func(w http.ResponseWriter, r *http.Request) {
		items, err := s.store.Environments(r.Context())
		if err != nil {
			problem(w, err)
			return
		}
		write(w, items)
	}))
	s.mux.HandleFunc("POST /api/environments", s.admin(func(w http.ResponseWriter, r *http.Request) {
		input, ok := decode[domain.Environment](w, r)
		if !ok {
			return
		}
		e, err := s.store.SaveEnvironment(r.Context(), input)
		if err != nil {
			problem(w, err)
			return
		}
		write(w, e)
	}))
	s.mux.HandleFunc("POST /api/environments/{id}/inspect", s.admin(func(w http.ResponseWriter, r *http.Request) {
		e, err := s.store.Environment(r.Context(), r.PathValue("id"))
		if err != nil {
			problem(w, err)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
		defer cancel()
		result, err := s.executor.Inspect(ctx, e)
		if err != nil {
			problem(w, err)
			return
		}
		write(w, result)
	}))
	s.mux.HandleFunc("GET /api/providers", s.admin(func(w http.ResponseWriter, r *http.Request) {
		items, err := s.store.Providers(r.Context())
		if err != nil {
			problem(w, err)
			return
		}
		write(w, items)
	}))
	s.mux.HandleFunc("POST /api/providers", s.admin(func(w http.ResponseWriter, r *http.Request) {
		input, ok := decode[domain.ProviderInput](w, r)
		if !ok {
			return
		}
		p, err := s.store.SaveProvider(r.Context(), input)
		if err != nil {
			problem(w, err)
			return
		}
		write(w, p)
	}))
	s.mux.HandleFunc("GET /api/models", s.admin(func(w http.ResponseWriter, r *http.Request) {
		items, err := s.store.Models(r.Context())
		if err != nil {
			problem(w, err)
			return
		}
		write(w, items)
	}))
	s.mux.HandleFunc("POST /api/models", s.admin(func(w http.ResponseWriter, r *http.Request) {
		input, ok := decode[domain.Model](w, r)
		if !ok {
			return
		}
		m, err := s.store.SaveModel(r.Context(), input)
		if err != nil {
			problem(w, err)
			return
		}
		write(w, m)
	}))
	s.mux.HandleFunc("GET /api/benchmarks", func(w http.ResponseWriter, r *http.Request) {
		items, err := s.store.Suites(r.Context())
		if err != nil {
			problem(w, err)
			return
		}
		write(w, items)
	})
	s.mux.HandleFunc("GET /api/benchmarks/{id}/tasks", s.admin(func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("id") != "swebench-verified" {
			problem(w, domain.ErrNotFound)
			return
		}
		e, err := s.store.Environment(r.Context(), r.URL.Query().Get("environment_id"))
		if err != nil {
			problem(w, err)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
		defer cancel()
		items, err := s.executor.Tasks(ctx, e, domain.SWEBenchVerified())
		if err != nil {
			problem(w, err)
			return
		}
		write(w, items)
	}))
	s.mux.HandleFunc("GET /api/benchmarks/{id}/resolve", s.admin(func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("id") != domain.SWEBenchVerified().ID {
			problem(w, domain.ErrNotFound)
			return
		}
		e, err := s.store.Environment(r.Context(), r.URL.Query().Get("environment_id"))
		if err != nil {
			problem(w, err)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
		defer cancel()
		resolved, err := s.executor.Resolve(ctx, e, domain.SWEBenchVerified())
		if err != nil {
			problem(w, err)
			return
		}
		write(w, resolved)
	}))
	s.mux.HandleFunc("GET /api/runs", func(w http.ResponseWriter, r *http.Request) {
		limit, offset := pagination(r)
		items, err := s.store.Runs(r.Context(), limit, offset)
		if err != nil {
			problem(w, err)
			return
		}
		for i := range items.Items {
			items.Items[i] = publicRun(items.Items[i])
		}
		write(w, items)
	})
	s.mux.HandleFunc("GET /api/runs/{id}", func(w http.ResponseWriter, r *http.Request) {
		run, err := s.store.Run(r.Context(), r.PathValue("id"))
		if err != nil {
			problem(w, err)
			return
		}
		write(w, publicRun(run))
	})
	s.mux.HandleFunc("POST /api/runs", s.admin(s.createRun))
	s.mux.HandleFunc("POST /api/runs/{id}/start", s.admin(s.startRun))
	s.mux.HandleFunc("POST /api/runs/{id}/cancel", s.admin(s.cancelRun))
	s.mux.HandleFunc("GET /api/runs/{id}/events", func(w http.ResponseWriter, r *http.Request) {
		if _, err := s.store.Run(r.Context(), r.PathValue("id")); err != nil {
			problem(w, err)
			return
		}
		after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
		events, err := s.store.Events(r.Context(), r.PathValue("id"), after)
		if err != nil {
			problem(w, err)
			return
		}
		write(w, events)
	})
	s.mux.HandleFunc("GET /api/requests", func(w http.ResponseWriter, r *http.Request) {
		limit, offset := pagination(r)
		items, err := s.store.Requests(r.Context(), r.URL.Query().Get("run_id"), r.URL.Query().Get("attempt_id"), limit, offset)
		if err != nil {
			problem(w, err)
			return
		}
		write(w, items)
	})
	s.mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { fail(w, 404, "unknown API endpoint") })
	s.mux.HandleFunc("/", s.web)
}

func publicRun(r domain.Run) domain.Run {
	if u, err := url.Parse(r.Snapshot.Environment.ProxyURL); err == nil && u.User != nil {
		u.User = nil
		r.Snapshot.Environment.ProxyURL = u.String()
	}
	return r
}
func (s *Server) web(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		fail(w, 405, "read-only results application")
		return
	}
	if s.config.WebDir == "" {
		write(w, map[string]string{"message": "Build the results UI with bun run build:web, then start the backend with --web-dir."})
		return
	}
	clean := filepath.Clean(filepath.FromSlash(r.URL.Path))
	if strings.Contains(clean, "..") {
		fail(w, 400, "invalid asset path")
		return
	}
	root, err := filepath.Abs(s.config.WebDir)
	if err != nil {
		fail(w, 500, "results asset directory is unavailable")
		return
	}
	file := filepath.Join(root, strings.TrimLeft(clean, "\\/"))
	rel, err := filepath.Rel(root, file)
	if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		fail(w, 400, "invalid asset path")
		return
	}
	if info, err := os.Stat(file); err != nil || info.IsDir() {
		file = filepath.Join(root, "index.html")
	}
	if !regularAsset(root, file) {
		fail(w, 403, "symlink or nonregular assets are not served")
		return
	}
	assets, err := os.OpenRoot(root)
	if err != nil {
		fail(w, 500, "results asset directory is unavailable")
		return
	}
	defer assets.Close()
	rel, err = filepath.Rel(root, file)
	if err != nil {
		fail(w, 403, "invalid results asset")
		return
	}
	asset, err := assets.Open(rel)
	if err != nil {
		fail(w, 403, "results asset is unavailable")
		return
	}
	defer asset.Close()
	info, err := asset.Stat()
	if err != nil || !info.Mode().IsRegular() {
		fail(w, 403, "nonregular results asset")
		return
	}
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'")
	http.ServeContent(w, r, info.Name(), info.ModTime(), asset)
}

func regularAsset(root, file string) bool {
	for current := file; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return false
		}
		if current == root {
			return info.IsDir()
		}
		if current == file && !info.Mode().IsRegular() {
			return false
		}
		if filepath.Dir(current) == current {
			return false
		}
	}
}

func (s *Server) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		s.wg.Wait()
		return nil
	}
	s.closed = true
	for _, run := range s.active {
		run.cancel()
		if run.token != "" {
			s.gateway.Revoke(run.token)
		}
	}
	s.mu.Unlock()
	err := s.executor.Close()
	s.wg.Wait()
	return err
}
