package transcript

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToText_DedupesRollingAutoCaptions(t *testing.T) {
	tests := []struct {
		name string
		file string
		want string
	}{
		{
			name: "vtt rolling repeats",
			file: "auto_rolling.vtt",
			want: "hello there\nhow are you\ndoing today",
		},
		{
			name: "json3 growth and repeats",
			file: "auto_rolling.json3",
			want: "hello there\nhow are you doing today",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", tc.file))
			require.NoError(t, err)

			got, err := ToText(data)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestAssemble_DedupeRules(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want string
	}{
		{"empty", nil, ""},
		{"drops empties", []string{"", "a", "", "b"}, "a\nb"},
		{"exact consecutive repeat", []string{"a", "a", "b"}, "a\nb"},
		{"prefix growth keeps longest", []string{"how", "how are", "how are you"}, "how are you"},
		{"shorter prefix dropped", []string{"how are you", "how are"}, "how are you"},
		{"distinct lines kept", []string{"one", "two", "three"}, "one\ntwo\nthree"},
		{"non-consecutive repeat kept", []string{"a", "b", "a"}, "a\nb\na"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, assemble(tc.in))
		})
	}
}

func TestCleanLine(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"inline timing tag", "hello<00:00:00.500> there", "hello there"},
		{"styling tags", "how<c> are</c> you", "how are you"},
		{"entities", "a &amp; b &gt; c&nbsp;d", "a & b > c d"},
		{"collapse whitespace", "  too   many   spaces ", "too many spaces"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, cleanLine(tc.in))
		})
	}
}

func TestToText_UnrecognizedFormat(t *testing.T) {
	_, err := ToText([]byte("not a subtitle file"))
	require.Error(t, err)
}

func TestToText_ManualCaptionsNotOverDeduped(t *testing.T) {
	vtt := "WEBVTT\n\n" +
		"00:00:00.000 --> 00:00:02.000\nFirst sentence.\n\n" +
		"00:00:02.000 --> 00:00:04.000\nSecond sentence.\n\n" +
		"00:00:04.000 --> 00:00:06.000\nThird sentence.\n"
	got, err := ToText([]byte(vtt))
	require.NoError(t, err)
	assert.Equal(t, "First sentence.\nSecond sentence.\nThird sentence.", got)
}
