# transcrawl

Download the transcripts of the most recent videos of one or more YouTube
channels and save them as local files.

```bash
transcrawl --channels "https://www.youtube.com/@channelA,https://www.youtube.com/@channelB" --last 10
```

## Prerequisite: yt-dlp

transcrawl shells out to [`yt-dlp`](https://github.com/yt-dlp/yt-dlp) for all
YouTube access. **You must have `yt-dlp` installed and on your `PATH`.**

```bash
brew install yt-dlp        # macOS
pipx install yt-dlp        # cross-platform, isolated
pip install -U yt-dlp      # cross-platform
```

If `yt-dlp` is missing, transcrawl exits non-zero with install instructions
before doing any work.

**Why shell out instead of reimplementing?** YouTube changes its internals
constantly; `yt-dlp` is actively maintained to keep up. Reimplementing the
scraping in Go would mean chasing breakage forever. The tradeoff is the extra
runtime dependency. Subtitle fetching is hidden behind a `TranscriptFetcher`
interface (`internal/transcript`), so an alternate engine can be swapped in
without touching the rest of the code.

## Install / build

```bash
go build -o bin/transcrawl ./cmd/transcrawl
```

## Usage

```
transcrawl [flags] [channel-url...]
```

Channels may be passed positionally or via `--channels` (comma-separated).
Any form `yt-dlp` accepts works: `@handle`, `/channel/UC...`, `/c/custom`,
`/user/name`, or a full URL. transcrawl appends `/videos` automatically.

```bash
# 3 most recent, Ukrainian then English captions
transcrawl --last 3 --langs uk,en https://www.youtube.com/@SomeChannel

# multiple channels, SRT output, 8 in parallel
transcrawl -n 20 -f srt -c 8 @channelA @channelB
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--channels` | — | Comma-separated channel URLs/handles (alt to positional args) |
| `--last`, `-n` | `10` | Number of most recent videos per channel |
| `--langs`, `-l` | `en` | Comma-separated subtitle language priority, e.g. `uk,en` |
| `--out`, `-o` | `./transcripts` | Output root directory |
| `--format`, `-f` | `txt` | `txt` (clean text), `srt`, `vtt`, or `json` |
| `--concurrency`, `-c` | `4` | Number of videos fetched in parallel |
| `--sleep` | `1` | Seconds between yt-dlp requests (throttle avoidance) |
| `--manual-only` | `false` | Use only human captions; skip auto-generated |
| `--overwrite` | `false` | Re-fetch even if the output file already exists |
| `--manifest` | `true` | Write a `manifest.json` index per channel |
| `--verbose`, `-v` | `false` | Verbose logging |

## Output layout

```
transcripts/
├── @channelA/
│   ├── 20260601_video-title-slug_dQw4w9WgXcQ.txt
│   ├── 20260528_another-title_abc123XYZ.txt
│   └── manifest.json
└── @channelB/
    └── ...
```

Filenames are `<uploadDate>_<title-slug>_<videoID>.<ext>`. The video ID is
always included so files never collide, even with duplicate titles. Re-running
skips files that already exist (use `--overwrite` to force a re-fetch).

`manifest.json` indexes every enumerated video with its outcome:

```json
{
  "channel": "@channelA",
  "fetched_at": "2026-06-09T12:00:00Z",
  "videos": [
    {"id": "dQw4w9WgXcQ", "title": "...", "upload_date": "20260601",
     "file": "20260601_..._dQw4w9WgXcQ.txt", "status": "ok", "lang": "en"},
    {"id": "xyz", "status": "no_transcript"}
  ]
}
```

`status` is one of `ok`, `skipped`, `no_transcript`, or `failed`.

## Behavior notes

- **Caption preference:** manual captions win over auto-generated; languages
  are tried in `--langs` order. With `--manual-only`, auto-captions are skipped
  entirely.
- **Clean text (`txt`):** cue numbers, timestamps, and inline tags are
  stripped, and the rolling repeats YouTube auto-captions emit are
  de-duplicated into continuous prose.
- **Resilience:** one failing video never aborts the run. Transient failures
  (HTTP 429, network blips) are retried up to 3× with exponential backoff;
  everything else is logged and marked `failed`.
- **Ctrl-C** cancels cleanly and still writes manifests for completed work.
- **Live/upcoming** streams are skipped; channels with fewer than `--last`
  videos fetch whatever exists.

## Development

```bash
go build -o bin/transcrawl ./cmd/transcrawl
go test ./...
go vet ./...
```

The auto-caption dedupe logic is covered by table-driven tests against
fixtures in `internal/transcript/testdata/`.
