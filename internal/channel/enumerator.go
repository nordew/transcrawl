package channel

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

type Video struct {
	ID         string
	Title      string
	UploadDate string
	LiveStatus string
}

func (v Video) IsLiveOrUpcoming() bool {
	return v.LiveStatus == "is_live" || v.LiveStatus == "is_upcoming"
}

type runner interface {
	Run(ctx context.Context, args ...string) (string, error)
}

type Enumerator struct {
	r runner
}

func NewEnumerator(r runner) *Enumerator { return &Enumerator{r: r} }

func (e *Enumerator) Enumerate(ctx context.Context, channelURL string, n int) ([]Video, error) {
	listURL := normalizeVideosURL(channelURL)

	out, err := e.r.Run(ctx,
		"--flat-playlist",
		"--ignore-errors",
		"--playlist-end", strconv.Itoa(n),
		"--print", "%(id)s\t%(title)s\t%(upload_date)s\t%(live_status)s",
		listURL,
	)
	if err != nil {
		return nil, fmt.Errorf("listing %q: %w", listURL, err)
	}

	var videos []Video
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 4)
		if len(parts) < 1 || parts[0] == "" {
			continue
		}
		v := Video{ID: parts[0]}
		if len(parts) > 1 {
			v.Title = parts[1]
		}
		if len(parts) > 2 {
			v.UploadDate = blankNA(parts[2])
		}
		if len(parts) > 3 {
			v.LiveStatus = blankNA(parts[3])
		}
		videos = append(videos, v)
	}
	return videos, nil
}

func blankNA(s string) string {
	if s == "NA" {
		return ""
	}
	return s
}

func normalizeVideosURL(raw string) string {
	s := strings.TrimSpace(raw)

	if !strings.Contains(s, "youtube.com") && !strings.Contains(s, "youtu.be") {
		switch {
		case strings.HasPrefix(s, "@"):
			s = "https://www.youtube.com/" + s
		case strings.HasPrefix(s, "UC") && len(s) == 24:
			s = "https://www.youtube.com/channel/" + s
		default:
			s = "https://www.youtube.com/@" + strings.TrimPrefix(s, "@")
		}
	} else if !strings.Contains(s, "://") {
		s = "https://" + s
	}

	trimmed := strings.TrimRight(s, "/")
	for _, tab := range []string{"/videos", "/streams", "/shorts", "/playlists", "/featured", "/search"} {
		if strings.HasSuffix(trimmed, tab) || strings.Contains(trimmed, tab+"?") {
			return trimmed
		}
	}
	return trimmed + "/videos"
}

func Handle(raw string) string {
	s := strings.TrimSpace(raw)
	if i := strings.IndexByte(s, '@'); i >= 0 {
		rest := s[i+1:]
		rest = cutAny(rest, "/?#")
		if rest != "" {
			return "@" + sanitize(rest)
		}
	}

	if u, err := url.Parse(ensureScheme(s)); err == nil {
		segs := strings.Split(strings.Trim(u.Path, "/"), "/")
		for i, seg := range segs {
			switch seg {
			case "channel", "c", "user":
				if i+1 < len(segs) {
					return sanitize(segs[i+1])
				}
			}
		}
		if len(segs) > 0 && segs[0] != "" {
			return sanitize(segs[0])
		}
	}
	return sanitize(s)
}

func ensureScheme(s string) string {
	if !strings.Contains(s, "://") {
		return "https://" + s
	}
	return s
}

func cutAny(s, chars string) string {
	if i := strings.IndexAny(s, chars); i >= 0 {
		return s[:i]
	}
	return s
}

func sanitize(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		case r == '-' || r == '_' || r == '.':
			return r
		default:
			return '-'
		}
	}, s)
	s = strings.Trim(s, "-._")
	if s == "" {
		return "channel"
	}
	return s
}
