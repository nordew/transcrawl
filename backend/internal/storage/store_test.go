package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSlug(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"simple", "Hello World", "hello-world"},
		{"unsafe chars dropped", "a/b\\c:d?e", "a-b-cde"},
		{"collapse separators", "a   ---  b", "a-b"},
		{"trim dashes", "  spaced  ", "spaced"},
		{"ukrainian transliterated", "Привіт світ", "privit-svit"},
		{"russian transliterated", "Деньги это энергия", "dengi-eto-energiia"},
		{"emoji-only fallback", "🎬🔥", "video"},
		{"keeps digits", "Top 10 things", "top-10-things"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, Slug(tc.in))
		})
	}
}

func TestSlug_Truncates(t *testing.T) {
	long := ""
	for range 200 {
		long += "a"
	}
	assert.LessOrEqual(t, len(Slug(long)), maxSlugLen)
}

func TestFilename(t *testing.T) {
	got := Filename("20260601", "Some Title!", "dQw4w9WgXcQ", "txt")
	assert.Equal(t, "20260601_some-title_dQw4w9WgXcQ.txt", got)

	got = Filename("", "x", "abc", "srt")
	assert.Equal(t, "00000000_x_abc.srt", got)
}

func TestWriteManifest(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	at := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)

	err := s.WriteManifest(dir, "@chan", at, []ManifestVideo{
		{ID: "a", Title: "T", UploadDate: "20260601", File: "f.txt", Status: "ok", Lang: "en"},
		{ID: "b", Status: "no_transcript"},
	})
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	require.NoError(t, err)

	var m Manifest
	require.NoError(t, json.Unmarshal(data, &m))
	assert.Equal(t, "@chan", m.Channel)
	assert.Equal(t, "2026-06-09T12:00:00Z", m.FetchedAt)
	require.Len(t, m.Videos, 2)
	assert.Equal(t, "en", m.Videos[0].Lang)
	assert.NotContains(t, string(data), `"lang": ""`)
}

func TestExists(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.txt")
	assert.False(t, Exists(p))
	require.NoError(t, Write(p, "hi"))
	assert.True(t, Exists(p))
}
