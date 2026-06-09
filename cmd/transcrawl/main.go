package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/romanhorishnyi/transcrawl/internal/app"
	"github.com/romanhorishnyi/transcrawl/internal/channel"
	"github.com/romanhorishnyi/transcrawl/internal/config"
	"github.com/romanhorishnyi/transcrawl/internal/storage"
	"github.com/romanhorishnyi/transcrawl/internal/transcript"
	"github.com/romanhorishnyi/transcrawl/internal/ytdlp"
	"github.com/spf13/cobra"
	"golang.org/x/time/rate"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "transcrawl:", err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	var raw config.Raw
	cmd := &cobra.Command{
		Use:   "transcrawl [flags] [channel-url...]",
		Short: "Download transcripts of recent YouTube videos via yt-dlp",
		Long: "transcrawl fetches the N most recent videos of each channel and saves\n" +
			"their transcripts as local files. Channels may be passed positionally or\n" +
			"via --channels. Requires yt-dlp on PATH.",
		Args:          cobra.ArbitraryArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			raw.Positional = args
			cfg, err := config.Build(raw)
			if err != nil {
				return err
			}
			return run(cmd.Context(), cfg)
		},
	}

	f := cmd.Flags()
	f.StringVar(&raw.Channels, "channels", "", "comma-separated channel URLs/handles (alt to positional args)")
	f.IntVarP(&raw.Last, "last", "n", 10, "number of most recent videos per channel")
	f.StringVarP(&raw.Langs, "langs", "l", "en", "comma-separated subtitle language priority, e.g. uk,en")
	f.StringVarP(&raw.Out, "out", "o", "./transcripts", "output root directory")
	f.StringVarP(&raw.Format, "format", "f", "txt", "transcript format: txt, srt, vtt or json")
	f.IntVarP(&raw.Concurrency, "concurrency", "c", 4, "number of videos fetched in parallel")
	f.IntVar(&raw.Sleep, "sleep", 1, "seconds between yt-dlp requests (throttle avoidance)")
	f.BoolVar(&raw.ManualOnly, "manual-only", false, "use only human captions; skip auto-generated")
	f.BoolVar(&raw.Overwrite, "overwrite", false, "re-fetch even if the output file already exists")
	f.BoolVar(&raw.Manifest, "manifest", true, "write a manifest.json index per channel")
	f.BoolVarP(&raw.Verbose, "verbose", "v", false, "verbose logging")

	return cmd
}

func run(parent context.Context, cfg config.Config) error {
	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()

	log := newLogger(cfg.Verbose)

	var limiter *rate.Limiter
	if cfg.Sleep > 0 {
		limiter = rate.NewLimiter(rate.Every(cfg.Sleep), 1)
	}
	runner := ytdlp.New(limiter)

	version, err := runner.CheckInstalled(ctx)
	if err != nil {
		return fmt.Errorf("%w\n\nyt-dlp is required. Install it and ensure it is on PATH:\n"+
			"  https://github.com/yt-dlp/yt-dlp#installation\n"+
			"  (e.g. `brew install yt-dlp`, `pipx install yt-dlp`, or `pip install -U yt-dlp`)", err)
	}
	log.Debug("yt-dlp detected", "version", version)

	deps := app.Deps{
		Logger:     log,
		Enumerator: channel.NewEnumerator(runner),
		Fetcher:    transcript.NewYTDLPFetcher(runner),
		Store:      storage.New(cfg.Out),
		Now:        time.Now,
	}

	sum, err := app.Run(ctx, cfg, deps)
	if err != nil {
		return err
	}
	log.Info("done", "summary", sum.String())
	fmt.Println(sum.String())
	return nil
}

func newLogger(verbose bool) *slog.Logger {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}
