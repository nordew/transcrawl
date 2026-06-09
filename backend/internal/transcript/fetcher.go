package transcript

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/romanhorishnyi/transcrawl/internal/channel"
)

var ErrNoTranscript = errors.New("no transcript available")

type Options struct {
	Langs      []string
	Format     string
	ManualOnly bool
}

type Result struct {
	Text string
	Lang string
	Ext  string
}

type Fetcher interface {
	Fetch(ctx context.Context, v channel.Video, opts Options) (Result, error)
}

type runner interface {
	Run(ctx context.Context, args ...string) (string, error)
}

type YTDLPFetcher struct {
	r runner
}

func NewYTDLPFetcher(r runner) *YTDLPFetcher { return &YTDLPFetcher{r: r} }

var knownSubExts = map[string]bool{"json3": true, "vtt": true, "srt": true, "ttml": true, "srv3": true}

func (f *YTDLPFetcher) Fetch(ctx context.Context, v channel.Video, opts Options) (Result, error) {
	tmp, err := os.MkdirTemp("", "transcrawl-"+v.ID+"-")
	if err != nil {
		return Result{}, fmt.Errorf("temp dir: %w", err)
	}
	defer os.RemoveAll(tmp)

	args := []string{
		"--skip-download",
		"--write-subs",
		"--no-warnings",
		"--sub-langs", strings.Join(opts.Langs, ","),
		"-o", filepath.Join(tmp, "%(id)s.%(ext)s"),
	}
	if !opts.ManualOnly {
		args = append(args, "--write-auto-subs")
	}
	args = append(args, subFormatArgs(opts.Format)...)
	args = append(args, "https://www.youtube.com/watch?v="+v.ID)

	_, runErr := f.r.Run(ctx, args...)

	cand, selErr := selectSubtitle(tmp, v.ID, opts.Langs)
	if selErr != nil {
		if runErr != nil {
			return Result{}, runErr
		}
		return Result{}, ErrNoTranscript
	}

	data, err := os.ReadFile(cand.path)
	if err != nil {
		return Result{}, fmt.Errorf("reading subtitle %s: %w", cand.path, err)
	}

	if opts.Format == "txt" {
		text, err := ToText(data)
		if err != nil {
			return Result{}, fmt.Errorf("converting %s to text: %w", filepath.Base(cand.path), err)
		}
		return Result{Text: text, Lang: cand.lang, Ext: "txt"}, nil
	}
	return Result{Text: string(data), Lang: cand.lang, Ext: opts.Format}, nil
}

func subFormatArgs(format string) []string {
	switch format {
	case "txt":
		return []string{"--sub-format", "json3/vtt"}
	case "vtt":
		return []string{"--sub-format", "vtt"}
	case "json":
		return []string{"--sub-format", "json3"}
	case "srt":
		return []string{"--sub-format", "vtt/srt", "--convert-subs", "srt"}
	default:
		return []string{"--sub-format", "json3/vtt"}
	}
}

type subFile struct {
	path string
	lang string
	ext  string
}

func selectSubtitle(tmp, id string, langs []string) (subFile, error) {
	entries, err := os.ReadDir(tmp)
	if err != nil {
		return subFile{}, err
	}

	prefix := id + "."
	var found []subFile
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		rest := strings.TrimPrefix(name, prefix)
		dot := strings.LastIndexByte(rest, '.')
		if dot < 0 {
			continue
		}
		lang, ext := rest[:dot], rest[dot+1:]
		if !knownSubExts[ext] {
			continue
		}
		found = append(found, subFile{path: filepath.Join(tmp, name), lang: lang, ext: ext})
	}
	if len(found) == 0 {
		return subFile{}, ErrNoTranscript
	}

	sort.SliceStable(found, func(i, j int) bool {
		return extRank(found[i].ext) < extRank(found[j].ext)
	})

	for _, want := range langs {
		for _, sf := range found {
			if langMatches(sf.lang, want) {
				return sf, nil
			}
		}
	}
	return found[0], nil
}

func extRank(ext string) int {
	switch ext {
	case "json3":
		return 0
	case "vtt":
		return 1
	case "srt":
		return 2
	default:
		return 3
	}
}

func langMatches(got, want string) bool {
	got = strings.ToLower(got)
	want = strings.ToLower(want)
	return got == want || strings.HasPrefix(got, want+"-")
}
