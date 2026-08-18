// Package brands ships per-kind brand marks baked into the binary. The marks
// are the official favicons/press assets of each product (claude.ai, openai.com,
// grok.com, pi.dev, …), fetched once during development and embedded: browse
// must work offline and must not depend on favicon endpoints staying alive.
package brands

import "embed"

//go:embed logos/*.png
var logos embed.FS

// Kinds lists every kind that has an embedded mark, in a stable order (the
// kitty image ids are assigned from it).
var Kinds = []string{
	"claude", "codex", "grok", "pi",
	"kimi", "copilot", "cursor", "droid", "devin",
	"opencode", "kilo", "qodercli", "antigravity",
}

// Logo returns the embedded brand mark for an agent kind. ok=false means the
// kind has no mark (callers fall back to the colored glyph).
func Logo(kind string) ([]byte, bool) {
	if kind == "" {
		return nil, false
	}
	b, err := logos.ReadFile("logos/" + kind + ".png")
	return b, err == nil && len(b) > 0
}
