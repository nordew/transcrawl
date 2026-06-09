# transcrawl

Download the transcripts of the most recent videos of one or more YouTube
channels and save them as local files — from the command line or a small web UI.

```bash
# CLI
transcrawl --channels "https://www.youtube.com/@channelA,https://www.youtube.com/@channelB" --last 10

# Web UI
transcrawl serve            # then open http://localhost:8080
```

## Layout

```
transcrawl/
├── backend/      Go module: CLI + HTTP API (single binary)
│   ├── cmd/transcrawl/   wiring
│   └── internal/         channel, transcript, storage, app, server, ytdlp
└── frontend/     static web UI (no build step): index.html, styles.css, app.js
```

The backend serves the `frontend/` directory as-is; there is no bundler or npm.

## Prerequisite: yt-dlp

transcrawl shells out to [`yt-dlp`](https://github.com/yt-dlp/yt-dlp) for all
YouTube access. **You must have `yt-dlp` installed and on your `PATH`.**

```bash
brew install yt-dlp        # macOS
pipx install yt-dlp        # cross-platform, isolated
pip install -U yt-dlp      # cross-platform
```

If `yt-dlp` is missing, transcrawl exits non-zero (CLI) or streams an error
event (web) with install instructions before doing any work.

**Why shell out instead of reimplementing?** YouTube changes its internals
constantly; `yt-dlp` is actively maintained to keep up. Reimplementing the
scraping in Go would mean chasing breakage forever. The tradeoff is the extra
runtime dependency. Subtitle fetching is hidden behind a `TranscriptFetcher`
interface (`internal/transcript`), so an alternate engine can be swapped in
without touching the rest of the code.

## Build

```bash
cd backend
go build -o bin/transcrawl ./cmd/transcrawl
```

## Web UI

```bash
# run from the repo root so the default --web path resolves
./backend/bin/transcrawl serve
```

Open <http://localhost:8080>, paste one or more channels, and pull their latest
transcripts. Progress streams live (Server-Sent Events): each video appears as
it finishes with a status, and saved transcripts can be viewed in a reading
drawer or downloaded. Files are written to the output directory and reused on
the next run, exactly like the CLI.

| serve flag | Default | Description |
|------------|---------|-------------|
| `--addr` | `:8080` | Address to listen on |
| `--out`, `-o` | `./transcripts` | Output root directory |
| `--web` | `frontend` | Directory of frontend static files |

The `--web` default is relative to the current directory, so run `serve` from
the repo root (or pass an absolute `--web` path).

### HTTP API

- `GET /api/stream?channels=&last=&langs=&format=&manual_only=&overwrite=` —
  Server-Sent Events: `enumerated`, `video`, `channel_error`, `done`, `error`.
- `GET /api/file?channel=<handle>&file=<name>[&download=1]` — serve a saved
  transcript (path-restricted to the output directory).

## CLI usage

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
always included so files never collide; Cyrillic titles are transliterated to
ASCII. Re-running skips files that already exist (use `--overwrite` to force a
re-fetch). `manifest.json` indexes every video with its outcome (`ok`,
`skipped`, `no_transcript`, `failed`).

## Behavior notes

- **Caption preference:** manual captions win over auto-generated; languages
  are tried in `--langs` order. `--manual-only` skips auto-captions entirely.
- **Clean text (`txt`):** cue numbers, timestamps, and inline tags are
  stripped, and the rolling repeats YouTube auto-captions emit are
  de-duplicated into continuous prose.
- **Resilience:** one failing video never aborts the run. Transient failures
  (HTTP 429, network blips) are retried up to 3× with exponential backoff.
- **Ctrl-C** (CLI) or closing the stream (web) cancels cleanly and still writes
  manifests for completed work.
- **Live/upcoming** streams are skipped; channels with fewer than `--last`
  videos fetch whatever exists.

## Development

```bash
cd backend
go build ./...
go test ./...
go vet ./...
```

The auto-caption dedupe and slug transliteration are covered by table-driven
tests with fixtures under `internal/transcript/testdata/`.
