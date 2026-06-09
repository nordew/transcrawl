package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

const maxSlugLen = 80

type Store struct {
	root string
}

func New(root string) *Store { return &Store{root: root} }

func (s *Store) ChannelDir(handle string) (string, error) {
	dir := filepath.Join(s.root, handle)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating %q: %w", dir, err)
	}
	return dir, nil
}

func Filename(uploadDate, title, videoID, ext string) string {
	date := uploadDate
	if date == "" {
		date = "00000000"
	}
	return fmt.Sprintf("%s_%s_%s.%s", date, Slug(title), videoID, ext)
}

func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func Write(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func Slug(title string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(title) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		case unicode.IsSpace(r), r == '-', r == '_', r == '/', r == '\\', r == '.':
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	slug := strings.Trim(b.String(), "-")
	if len(slug) > maxSlugLen {
		slug = strings.Trim(slug[:maxSlugLen], "-")
	}
	if slug == "" {
		return "video"
	}
	return slug
}

type Manifest struct {
	Channel   string          `json:"channel"`
	FetchedAt string          `json:"fetched_at"`
	Videos    []ManifestVideo `json:"videos"`
}

type ManifestVideo struct {
	ID         string `json:"id"`
	Title      string `json:"title,omitempty"`
	UploadDate string `json:"upload_date,omitempty"`
	File       string `json:"file,omitempty"`
	Status     string `json:"status"`
	Lang       string `json:"lang,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

func (s *Store) WriteManifest(dir, channel string, fetchedAt time.Time, videos []ManifestVideo) error {
	m := Manifest{
		Channel:   channel,
		FetchedAt: fetchedAt.UTC().Format(time.RFC3339),
		Videos:    videos,
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling manifest: %w", err)
	}
	path := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("writing %q: %w", path, err)
	}
	return nil
}
