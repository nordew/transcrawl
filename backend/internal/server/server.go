package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/romanhorishnyi/transcrawl/internal/app"
	"github.com/romanhorishnyi/transcrawl/internal/channel"
	"github.com/romanhorishnyi/transcrawl/internal/config"
	"github.com/romanhorishnyi/transcrawl/internal/storage"
	"github.com/romanhorishnyi/transcrawl/internal/transcript"
	"github.com/romanhorishnyi/transcrawl/internal/ytdlp"
	"golang.org/x/time/rate"
)

type Server struct {
	log     *slog.Logger
	outRoot string
	webDir  string
}

func New(log *slog.Logger, outRoot, webDir string) *Server {
	return &Server{log: log, outRoot: outRoot, webDir: webDir}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/stream", s.handleStream)
	mux.HandleFunc("/api/file", s.handleFile)
	mux.Handle("/", http.FileServer(http.Dir(s.webDir)))
	return mux
}

func (s *Server) Run(ctx context.Context, addr string) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errc := make(chan error, 1)
	go func() {
		s.log.Info("serving", "addr", addr, "out", s.outRoot, "web", s.webDir)
		errc <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutCtx)
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

type sseEvent struct {
	Type    string                 `json:"type"`
	Channel string                 `json:"channel,omitempty"`
	URL     string                 `json:"url,omitempty"`
	Total   int                    `json:"total,omitempty"`
	Video   *storage.ManifestVideo `json:"video,omitempty"`
	Summary *app.Summary           `json:"summary,omitempty"`
	Message string                 `json:"message,omitempty"`
}

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	var mu sync.Mutex
	send := func(ev sseEvent) {
		mu.Lock()
		defer mu.Unlock()
		b, err := json.Marshal(ev)
		if err != nil {
			return
		}
		fmt.Fprintf(w, "data: %s\n\n", b)
		flusher.Flush()
	}

	cfg, err := s.configFromQuery(r)
	if err != nil {
		send(sseEvent{Type: "error", Message: err.Error()})
		return
	}

	var limiter *rate.Limiter
	if cfg.Sleep > 0 {
		limiter = rate.NewLimiter(rate.Every(cfg.Sleep), 1)
	}
	runner := ytdlp.New(limiter)
	if _, err := runner.CheckInstalled(r.Context()); err != nil {
		send(sseEvent{Type: "error", Message: "yt-dlp not available: " + err.Error()})
		return
	}

	deps := app.Deps{
		Logger:     s.log,
		Enumerator: channel.NewEnumerator(runner),
		Fetcher:    transcript.NewYTDLPFetcher(runner),
		Store:      storage.New(cfg.Out),
		Now:        time.Now,
		OnEnumerated: func(handle string, total int) {
			send(sseEvent{Type: "enumerated", Channel: handle, Total: total})
		},
		OnVideo: func(handle string, mv storage.ManifestVideo) {
			send(sseEvent{Type: "video", Channel: handle, Video: &mv})
		},
		OnChannelError: func(url string, err error) {
			send(sseEvent{Type: "channel_error", URL: url, Message: err.Error()})
		},
	}

	sum, err := app.Run(r.Context(), cfg, deps)
	if err != nil {
		send(sseEvent{Type: "error", Message: err.Error()})
		return
	}
	send(sseEvent{Type: "done", Summary: &sum})
}

func (s *Server) configFromQuery(r *http.Request) (config.Config, error) {
	q := r.URL.Query()
	raw := config.Raw{
		Channels:    q.Get("channels"),
		Last:        atoiDefault(q.Get("last"), 10),
		Langs:       valueOr(q.Get("langs"), "en"),
		Format:      valueOr(q.Get("format"), "txt"),
		Concurrency: atoiDefault(q.Get("concurrency"), 4),
		Sleep:       atoiDefault(q.Get("sleep"), 1),
		ManualOnly:  q.Get("manual_only") == "true",
		Overwrite:   q.Get("overwrite") == "true",
		Manifest:    true,
		Out:         s.outRoot,
	}
	return config.Build(raw)
}

func (s *Server) handleFile(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ch := filepath.Base(filepath.Clean("/" + q.Get("channel")))
	name := filepath.Base(filepath.Clean("/" + q.Get("file")))
	if ch == "." || ch == "/" || name == "." || name == "/" {
		http.Error(w, "bad path", http.StatusBadRequest)
		return
	}

	root, err := filepath.Abs(s.outRoot)
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	path := filepath.Join(root, ch, name)
	if !strings.HasPrefix(path, root+string(os.PathSeparator)) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	f, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if q.Get("download") == "1" {
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	}
	http.ServeContent(w, r, name, info.ModTime(), f)
}

func atoiDefault(s string, def int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
		return n
	}
	return def
}

func valueOr(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}
