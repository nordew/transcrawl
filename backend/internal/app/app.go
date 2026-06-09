package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/romanhorishnyi/transcrawl/internal/channel"
	"github.com/romanhorishnyi/transcrawl/internal/config"
	"github.com/romanhorishnyi/transcrawl/internal/storage"
	"github.com/romanhorishnyi/transcrawl/internal/transcript"
	"github.com/romanhorishnyi/transcrawl/internal/ytdlp"
)

const (
	maxAttempts    = 3
	defaultBackoff = time.Second
)

const (
	statusOK           = "ok"
	statusSkipped      = "skipped"
	statusNoTranscript = "no_transcript"
	statusFailed       = "failed"
)

type Enumerator interface {
	Enumerate(ctx context.Context, channelURL string, n int) ([]channel.Video, error)
}

type Deps struct {
	Logger       *slog.Logger
	Enumerator   Enumerator
	Fetcher      transcript.Fetcher
	Store        *storage.Store
	Now          func() time.Time
	RetryBackoff time.Duration

	OnEnumerated   func(handle string, total int)
	OnVideo        func(handle string, mv storage.ManifestVideo)
	OnChannelError func(url string, err error)
}

func (d Deps) emitEnumerated(handle string, total int) {
	if d.OnEnumerated != nil {
		d.OnEnumerated(handle, total)
	}
}

func (d Deps) emitVideo(handle string, mv storage.ManifestVideo) {
	if d.OnVideo != nil {
		d.OnVideo(handle, mv)
	}
}

func (d Deps) emitChannelError(url string, err error) {
	if d.OnChannelError != nil {
		d.OnChannelError(url, err)
	}
}

type Summary struct {
	OK           int `json:"ok"`
	Skipped      int `json:"skipped"`
	NoTranscript int `json:"no_transcript"`
	Failed       int `json:"failed"`
}

func (s Summary) String() string {
	return fmt.Sprintf("%d ok, %d skipped, %d no-transcript, %d failed",
		s.OK, s.Skipped, s.NoTranscript, s.Failed)
}

type channelWork struct {
	url      string
	handle   string
	dir      string
	videos   []channel.Video
	manifest []storage.ManifestVideo
}

type job struct {
	cw    *channelWork
	idx   int
	path  string
	fname string
}

func Run(ctx context.Context, cfg config.Config, deps Deps) (Summary, error) {
	works, jobs := plan(ctx, cfg, deps)

	if len(jobs) > 0 {
		runPool(ctx, cfg, deps, jobs)
	}

	finalize(works)
	writeManifests(cfg, deps, works)

	if err := ctx.Err(); err != nil {
		deps.Logger.Warn("run cancelled; wrote manifests for completed work", "err", err)
	}
	return tally(works), nil
}

func plan(ctx context.Context, cfg config.Config, deps Deps) ([]*channelWork, []job) {
	log := deps.Logger
	works := make([]*channelWork, 0, len(cfg.Channels))
	var jobs []job

	for _, url := range cfg.Channels {
		if ctx.Err() != nil {
			break
		}
		handle := channel.Handle(url)
		videos, err := deps.Enumerator.Enumerate(ctx, url, cfg.Last)
		if err != nil {
			log.Error("enumerating channel failed; skipping", "channel", url, "err", err)
			deps.emitChannelError(url, err)
			continue
		}
		log.Info("enumerated channel", "channel", url, "handle", handle, "videos", len(videos))
		deps.emitEnumerated(handle, len(videos))
		if len(videos) == 0 {
			log.Warn("channel has no videos", "channel", url)
			continue
		}
		dir, err := deps.Store.ChannelDir(handle)
		if err != nil {
			log.Error("creating channel dir failed; skipping", "channel", url, "err", err)
			deps.emitChannelError(url, err)
			continue
		}

		cw := &channelWork{
			url:      url,
			handle:   handle,
			dir:      dir,
			videos:   videos,
			manifest: make([]storage.ManifestVideo, len(videos)),
		}
		works = append(works, cw)

		for j, v := range videos {
			mv := storage.ManifestVideo{ID: v.ID, Title: v.Title, UploadDate: v.UploadDate}

			if v.IsLiveOrUpcoming() {
				log.Info("skipping live/upcoming stream", "id", v.ID, "status", v.LiveStatus)
				mv.Status = statusSkipped
				mv.Reason = "live or upcoming stream"
				cw.manifest[j] = mv
				deps.emitVideo(handle, mv)
				continue
			}

			fname := storage.Filename(v.UploadDate, v.Title, v.ID, cfg.Ext())
			path := filepath.Join(dir, fname)
			if !cfg.Overwrite && storage.Exists(path) {
				log.Debug("skipping existing transcript", "file", fname)
				mv.Status = statusSkipped
				mv.File = fname
				cw.manifest[j] = mv
				deps.emitVideo(handle, mv)
				continue
			}

			cw.manifest[j] = mv
			jobs = append(jobs, job{cw: cw, idx: j, path: path, fname: fname})
		}
	}
	return works, jobs
}

func runPool(ctx context.Context, cfg config.Config, deps Deps, jobs []job) {
	jobCh := make(chan job)
	opts := transcript.Options{Langs: cfg.Langs, Format: cfg.Ext(), ManualOnly: cfg.ManualOnly}

	var wg sync.WaitGroup
	workers := cfg.Concurrency
	if workers > len(jobs) {
		workers = len(jobs)
	}
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobCh {
				mv := process(ctx, deps, opts, j)
				j.cw.manifest[j.idx] = mv
				deps.emitVideo(j.cw.handle, mv)
			}
		}()
	}

	for _, j := range jobs {
		select {
		case <-ctx.Done():
			close(jobCh)
			wg.Wait()
			return
		case jobCh <- j:
		}
	}
	close(jobCh)
	wg.Wait()
}

func process(ctx context.Context, deps Deps, opts transcript.Options, j job) storage.ManifestVideo {
	v := j.cw.videos[j.idx]
	mv := storage.ManifestVideo{ID: v.ID, Title: v.Title, UploadDate: v.UploadDate}
	log := deps.Logger

	res, err := fetchWithRetry(ctx, deps, opts, v)
	switch {
	case err == nil:
		if err := storage.Write(j.path, res.Text); err != nil {
			log.Error("writing transcript failed", "id", v.ID, "err", err)
			mv.Status = statusFailed
			mv.Reason = "write: " + err.Error()
			return mv
		}
		log.Info("saved transcript", "id", v.ID, "lang", res.Lang, "file", j.fname)
		mv.Status = statusOK
		mv.File = j.fname
		mv.Lang = res.Lang
		return mv

	case errors.Is(err, transcript.ErrNoTranscript):
		log.Info("no transcript available", "id", v.ID)
		mv.Status = statusNoTranscript
		return mv

	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		mv.Status = statusFailed
		mv.Reason = "cancelled"
		return mv

	default:
		log.Warn("fetch failed", "id", v.ID, "err", err)
		mv.Status = statusFailed
		mv.Reason = shortReason(err)
		return mv
	}
}

func fetchWithRetry(ctx context.Context, deps Deps, opts transcript.Options, v channel.Video) (transcript.Result, error) {
	backoff := deps.RetryBackoff
	if backoff <= 0 {
		backoff = defaultBackoff
	}
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		res, err := deps.Fetcher.Fetch(ctx, v, opts)
		if err == nil {
			return res, nil
		}
		if errors.Is(err, transcript.ErrNoTranscript) || !ytdlp.IsTransient(err) {
			return transcript.Result{}, err
		}
		lastErr = err
		if attempt == maxAttempts {
			break
		}
		deps.Logger.Debug("transient fetch error; retrying", "id", v.ID, "attempt", attempt, "backoff", backoff)
		select {
		case <-ctx.Done():
			return transcript.Result{}, ctx.Err()
		case <-time.After(backoff):
		}
		backoff *= 2
	}
	return transcript.Result{}, fmt.Errorf("after %d attempts: %w", maxAttempts, lastErr)
}

func finalize(works []*channelWork) {
	for _, cw := range works {
		for i := range cw.manifest {
			if cw.manifest[i].Status == "" {
				cw.manifest[i].Status = statusSkipped
				cw.manifest[i].Reason = "cancelled before fetch"
			}
		}
	}
}

func writeManifests(cfg config.Config, deps Deps, works []*channelWork) {
	if !cfg.Manifest {
		return
	}
	now := deps.Now()
	for _, cw := range works {
		if err := deps.Store.WriteManifest(cw.dir, cw.handle, now, cw.manifest); err != nil {
			deps.Logger.Error("writing manifest failed", "channel", cw.handle, "err", err)
		}
	}
}

func tally(works []*channelWork) Summary {
	var s Summary
	for _, cw := range works {
		for _, mv := range cw.manifest {
			switch mv.Status {
			case statusOK:
				s.OK++
			case statusSkipped:
				s.Skipped++
			case statusNoTranscript:
				s.NoTranscript++
			case statusFailed:
				s.Failed++
			}
		}
	}
	return s
}

func shortReason(err error) string {
	var re *ytdlp.RunError
	if errors.As(err, &re) {
		return re.Error()
	}
	return err.Error()
}
