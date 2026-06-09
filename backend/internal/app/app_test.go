package app

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/romanhorishnyi/transcrawl/internal/channel"
	"github.com/romanhorishnyi/transcrawl/internal/config"
	"github.com/romanhorishnyi/transcrawl/internal/storage"
	"github.com/romanhorishnyi/transcrawl/internal/transcript"
	"github.com/romanhorishnyi/transcrawl/internal/ytdlp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

type fakeEnumerator struct {
	videos []channel.Video
}

func (f *fakeEnumerator) Enumerate(context.Context, string, int) ([]channel.Video, error) {
	return f.videos, nil
}

type fakeFetcher struct {
	mu     sync.Mutex
	calls  map[string]int
	behave func(id string, call int) (transcript.Result, error)
}

func (f *fakeFetcher) Fetch(_ context.Context, v channel.Video, _ transcript.Options) (transcript.Result, error) {
	f.mu.Lock()
	f.calls[v.ID]++
	call := f.calls[v.ID]
	f.mu.Unlock()
	return f.behave(v.ID, call)
}

func transientErr() error {
	return &ytdlp.RunError{Stderr: "ERROR: HTTP Error 429: Too Many Requests", Err: io.EOF}
}

func TestRun_MixedOutcomes(t *testing.T) {
	out := t.TempDir()
	url := "https://www.youtube.com/@chan"

	videos := []channel.Video{
		{ID: "ok1", Title: "Good One", UploadDate: "20260601"},
		{ID: "none", Title: "No Caps", UploadDate: "20260531"},
		{ID: "retry", Title: "Flaky", UploadDate: "20260530"},
		{ID: "fail", Title: "Private", UploadDate: "20260529"},
		{ID: "exists", Title: "Already Here", UploadDate: "20260528"},
		{ID: "live", Title: "Live Now", UploadDate: "", LiveStatus: "is_live"},
	}

	handle := channel.Handle(url)
	chanDir := filepath.Join(out, handle)
	require.NoError(t, os.MkdirAll(chanDir, 0o755))
	existsName := storage.Filename("20260528", "Already Here", "exists", "txt")
	require.NoError(t, storage.Write(filepath.Join(chanDir, existsName), "old"))

	fetcher := &fakeFetcher{
		calls: map[string]int{},
		behave: func(id string, call int) (transcript.Result, error) {
			switch id {
			case "ok1":
				return transcript.Result{Text: "hello world", Lang: "en", Ext: "txt"}, nil
			case "none":
				return transcript.Result{}, transcript.ErrNoTranscript
			case "retry":
				if call == 1 {
					return transcript.Result{}, transientErr()
				}
				return transcript.Result{Text: "recovered", Lang: "uk", Ext: "txt"}, nil
			case "fail":
				return transcript.Result{}, &ytdlp.RunError{Stderr: "ERROR: Private video", Err: io.EOF}
			default:
				t.Errorf("unexpected fetch for %q", id)
				return transcript.Result{}, transcript.ErrNoTranscript
			}
		},
	}

	cfg := config.Config{
		Channels:    []string{url},
		Last:        10,
		Langs:       []string{"uk", "en"},
		Out:         out,
		Format:      config.FormatTXT,
		Concurrency: 3,
		Manifest:    true,
	}
	deps := Deps{
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		Enumerator:   &fakeEnumerator{videos: videos},
		Fetcher:      fetcher,
		Store:        storage.New(out),
		Now:          func() time.Time { return time.Date(2026, 6, 9, 0, 0, 0, 0, time.UTC) },
		RetryBackoff: time.Millisecond,
	}

	sum, err := Run(context.Background(), cfg, deps)
	require.NoError(t, err)

	assert.Equal(t, Summary{OK: 2, Skipped: 2, NoTranscript: 1, Failed: 1}, sum)

	assert.Equal(t, 2, fetcher.calls["retry"])
	assert.Zero(t, fetcher.calls["exists"])
	assert.Zero(t, fetcher.calls["live"])

	okPath := filepath.Join(chanDir, storage.Filename("20260601", "Good One", "ok1", "txt"))
	content, err := os.ReadFile(okPath)
	require.NoError(t, err)
	assert.Equal(t, "hello world", string(content))

	data, err := os.ReadFile(filepath.Join(chanDir, "manifest.json"))
	require.NoError(t, err)
	var m storage.Manifest
	require.NoError(t, json.Unmarshal(data, &m))
	require.Len(t, m.Videos, 6)
	assert.Equal(t, "@chan", m.Channel)

	byID := map[string]storage.ManifestVideo{}
	for _, v := range m.Videos {
		byID[v.ID] = v
	}
	assert.Equal(t, statusOK, byID["ok1"].Status)
	assert.Equal(t, "uk", byID["retry"].Lang)
	assert.Equal(t, statusNoTranscript, byID["none"].Status)
	assert.Equal(t, statusFailed, byID["fail"].Status)
	assert.Equal(t, statusSkipped, byID["exists"].Status)
	assert.Equal(t, statusSkipped, byID["live"].Status)
	assert.Equal(t, "live or upcoming stream", byID["live"].Reason)
}

func TestRun_NoManifestFlag(t *testing.T) {
	out := t.TempDir()
	cfg := config.Config{
		Channels:    []string{"https://www.youtube.com/@c"},
		Last:        5,
		Langs:       []string{"en"},
		Out:         out,
		Format:      config.FormatTXT,
		Concurrency: 1,
		Manifest:    false,
	}
	deps := Deps{
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Enumerator: &fakeEnumerator{videos: []channel.Video{{ID: "x", Title: "X", UploadDate: "20260101"}}},
		Fetcher: &fakeFetcher{calls: map[string]int{}, behave: func(string, int) (transcript.Result, error) {
			return transcript.Result{Text: "t", Lang: "en", Ext: "txt"}, nil
		}},
		Store: storage.New(out),
		Now:   func() time.Time { return time.Unix(0, 0).UTC() },
	}

	_, err := Run(context.Background(), cfg, deps)
	require.NoError(t, err)
	assert.False(t, storage.Exists(filepath.Join(out, "@c", "manifest.json")))
}
