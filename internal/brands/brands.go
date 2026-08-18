// Package brands fetches and caches per-kind brand icons (favicons). Icons
// are user-cache only: fetched at runtime from public favicon endpoints,
// never shipped in the repo.
package brands

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Domains maps agent kinds to favicon domains (google s2 resolves them).
var Domains = map[string]string{
	"claude":      "anthropic.com",
	"codex":       "openai.com",
	"pi":          "mariozechner.at",
	"grok":        "x.ai",
	"kimi":        "moonshot.ai",
	"copilot":     "github.com",
	"cursor":      "cursor.com",
	"droid":       "factory.ai",
	"devin":       "devin.ai",
	"opencode":    "opencode.ai",
	"kilo":        "kilocode.ai",
	"qodercli":    "qoder.com",
	"antigravity": "google.com",
	"omp":         "omp.dev",
}

func iconDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "herdr-archive", "icons")
}

// PNG returns cached icon bytes for a kind, fetching on first use. ok=false
// means no icon available (callers fall back to glyphs).
func PNG(kind string) ([]byte, bool) {
	if Domains[kind] == "" {
		return nil, false
	}
	path := filepath.Join(iconDir(), kind+".png")
	if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
		return b, true
	}
	if err := os.MkdirAll(iconDir(), 0o700); err != nil {
		return nil, false
	}
	url := fmt.Sprintf("https://www.google.com/s2/favicons?domain=%s&sz=128", Domains[kind])
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, false
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil || len(b) < 100 || b[0] != 0x89 || b[1] != 'P' {
		return nil, false // not a PNG
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return nil, false
	}
	return b, true
}
