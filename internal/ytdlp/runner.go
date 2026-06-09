package ytdlp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"golang.org/x/time/rate"
)

const binary = "yt-dlp"

type Runner struct {
	limiter *rate.Limiter
}

func New(limiter *rate.Limiter) *Runner {
	return &Runner{limiter: limiter}
}

type RunError struct {
	Args   []string
	Stderr string
	Err    error
}

func (e *RunError) Error() string {
	stderr := strings.TrimSpace(e.Stderr)
	if stderr != "" {
		if i := strings.LastIndexByte(stderr, '\n'); i >= 0 {
			stderr = stderr[i+1:]
		}
		return fmt.Sprintf("yt-dlp: %v: %s", e.Err, stderr)
	}
	return fmt.Sprintf("yt-dlp: %v", e.Err)
}

func (e *RunError) Unwrap() error { return e.Err }

func (r *Runner) Run(ctx context.Context, args ...string) (string, error) {
	if r.limiter != nil {
		if err := r.limiter.Wait(ctx); err != nil {
			return "", err
		}
	}

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		return stdout.String(), &RunError{Args: args, Stderr: stderr.String(), Err: err}
	}
	return stdout.String(), nil
}

func (r *Runner) CheckInstalled(ctx context.Context) (string, error) {
	if _, err := exec.LookPath(binary); err != nil {
		return "", fmt.Errorf("%s not found on PATH: %w", binary, err)
	}
	out, err := r.Run(ctx, "--version")
	if err != nil {
		return "", fmt.Errorf("running %s --version: %w", binary, err)
	}
	return strings.TrimSpace(out), nil
}

func IsTransient(err error) bool {
	var re *RunError
	if !errors.As(err, &re) {
		return false
	}
	s := strings.ToLower(re.Stderr)
	for _, marker := range []string{
		"429",
		"too many requests",
		"timed out",
		"timeout",
		"connection reset",
		"connection refused",
		"temporary failure",
		"read: connection",
		"the read operation",
		"unable to download webpage",
		"giving up after",
	} {
		if strings.Contains(s, marker) {
			return true
		}
	}
	return false
}
