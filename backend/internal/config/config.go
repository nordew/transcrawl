package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Format string

const (
	FormatTXT  Format = "txt"
	FormatSRT  Format = "srt"
	FormatVTT  Format = "vtt"
	FormatJSON Format = "json"
)

type Config struct {
	Channels    []string
	Last        int
	Langs       []string
	Out         string
	Format      Format
	Concurrency int
	Sleep       time.Duration
	ManualOnly  bool
	Overwrite   bool
	Manifest    bool
	Verbose     bool
}

type Raw struct {
	Channels    string
	Positional  []string
	Last        int
	Langs       string
	Out         string
	Format      string
	Concurrency int
	Sleep       int
	ManualOnly  bool
	Overwrite   bool
	Manifest    bool
	Verbose     bool
}

func Build(r Raw) (Config, error) {
	channels := append(splitCSV(r.Channels), r.Positional...)
	channels = dedupeStrings(channels)
	if len(channels) == 0 {
		return Config{}, fmt.Errorf("no channels given: pass --channels or positional URLs")
	}

	if r.Last < 1 {
		return Config{}, fmt.Errorf("--last must be >= 1, got %d", r.Last)
	}
	if r.Concurrency < 1 {
		return Config{}, fmt.Errorf("--concurrency must be >= 1, got %d", r.Concurrency)
	}
	if r.Sleep < 0 {
		return Config{}, fmt.Errorf("--sleep must be >= 0, got %d", r.Sleep)
	}

	langs := splitCSV(r.Langs)
	if len(langs) == 0 {
		langs = []string{"en"}
	}

	format := Format(strings.ToLower(strings.TrimSpace(r.Format)))
	switch format {
	case FormatTXT, FormatSRT, FormatVTT, FormatJSON:
	default:
		return Config{}, fmt.Errorf("invalid --format %q: want txt, srt, vtt or json", r.Format)
	}

	out := r.Out
	if out == "" {
		out = "./transcripts"
	}
	if err := ensureWritableDir(out); err != nil {
		return Config{}, fmt.Errorf("output dir %q: %w", out, err)
	}

	return Config{
		Channels:    channels,
		Last:        r.Last,
		Langs:       langs,
		Out:         out,
		Format:      format,
		Concurrency: r.Concurrency,
		Sleep:       time.Duration(r.Sleep) * time.Second,
		ManualOnly:  r.ManualOnly,
		Overwrite:   r.Overwrite,
		Manifest:    r.Manifest,
		Verbose:     r.Verbose,
	}, nil
}

func (c Config) Ext() string { return string(c.Format) }

func splitCSV(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func dedupeStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := in[:0]
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func ensureWritableDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	probe := filepath.Join(dir, ".transcrawl-write-probe")
	f, err := os.OpenFile(probe, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("not writable: %w", err)
	}
	_ = f.Close()
	_ = os.Remove(probe)
	return nil
}
