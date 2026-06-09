package channel

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeVideosURL(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"handle url", "https://www.youtube.com/@chan", "https://www.youtube.com/@chan/videos"},
		{"bare handle", "@chan", "https://www.youtube.com/@chan/videos"},
		{"bare name no at", "chan", "https://www.youtube.com/@chan/videos"},
		{"channel id", "UC1234567890abcdefghABCD", "https://www.youtube.com/channel/UC1234567890abcdefghABCD/videos"},
		{"short uc treated as handle", "UCBerkeley", "https://www.youtube.com/@UCBerkeley/videos"},
		{"channel id path", "https://www.youtube.com/channel/UCabc", "https://www.youtube.com/channel/UCabc/videos"},
		{"custom c path", "https://www.youtube.com/c/Custom", "https://www.youtube.com/c/Custom/videos"},
		{"already videos", "https://www.youtube.com/@chan/videos", "https://www.youtube.com/@chan/videos"},
		{"streams tab kept", "https://www.youtube.com/@chan/streams", "https://www.youtube.com/@chan/streams"},
		{"trailing slash", "https://www.youtube.com/@chan/", "https://www.youtube.com/@chan/videos"},
		{"no scheme", "youtube.com/@chan", "https://youtube.com/@chan/videos"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, normalizeVideosURL(tc.in))
		})
	}
}

func TestHandle(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"handle", "https://www.youtube.com/@channelA", "@channelA"},
		{"handle with videos", "https://www.youtube.com/@channelA/videos", "@channelA"},
		{"bare handle", "@channelA", "@channelA"},
		{"channel id", "https://www.youtube.com/channel/UCabc123", "UCabc123"},
		{"custom", "https://www.youtube.com/c/SomeName", "SomeName"},
		{"user", "https://www.youtube.com/user/Legacy", "Legacy"},
		{"unsafe chars sanitized", "@weird/name", "@weird"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, Handle(tc.in))
		})
	}
}

type fakeRunner struct {
	out  string
	err  error
	args []string
}

func (f *fakeRunner) Run(_ context.Context, args ...string) (string, error) {
	f.args = args
	return f.out, f.err
}

func TestEnumerate_ParsesTSV(t *testing.T) {
	fr := &fakeRunner{out: "id1\tTitle One\t20260601\tnot_live\n" +
		"id2\tTitle Two\tNA\tis_upcoming\n" +
		"\n" +
		"id3\tTitle Three\t20260520\tNA\n"}
	e := NewEnumerator(fr)

	videos, err := e.Enumerate(context.Background(), "https://www.youtube.com/@chan", 3)
	require.NoError(t, err)
	require.Len(t, videos, 3)

	assert.Equal(t, Video{ID: "id1", Title: "Title One", UploadDate: "20260601", LiveStatus: "not_live"}, videos[0])
	assert.True(t, videos[1].IsLiveOrUpcoming())
	assert.Empty(t, videos[1].UploadDate)
	assert.Empty(t, videos[2].LiveStatus)
	assert.Contains(t, fr.args, "--flat-playlist")
	assert.Contains(t, fr.args, "https://www.youtube.com/@chan/videos")
}
