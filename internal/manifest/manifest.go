// Package manifest defines the herdr-archive snapshot format and its
// resurrect-style storage: an append-only directory of timestamped JSON
// files with a repointable `last` symlink for point-in-time restore.
package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Version is the snapshot format version.
const Version = 1

// Snapshot is one capture of a herdr session.
type Snapshot struct {
	Version    int         `json:"version"`
	CreatedAt  time.Time   `json:"created_at"`
	Session    string      `json:"session"` // "default" or a named session
	SessionDir string      `json:"session_dir,omitempty"`
	Workspaces []Workspace `json:"workspaces"`
}

// Workspace is a captured workspace; cwd is derived from its first pane.
type Workspace struct {
	ID    string `json:"id"`
	Label string `json:"label,omitempty"`
	Cwd   string `json:"cwd,omitempty"`
	Tabs  []Tab  `json:"tabs"`
}

// Tab is a captured tab.
type Tab struct {
	ID    string `json:"id"`
	Label string `json:"label,omitempty"`
	Panes []Pane `json:"panes"`
}

// Pane is a captured pane. Key is the stable identity used across restores:
// agent name when present, else the pane label, else the pane id. Pane IDs
// are never reused by herdr and are informational only.
type Pane struct {
	Key       string            `json:"key"`
	PaneID    string            `json:"pane_id"` // informational, never identity
	Agent     string            `json:"agent,omitempty"`  // detected kind (claude, codex, pi...)
	Name      string            `json:"name,omitempty"`   // herdr agent name
	State     string            `json:"state,omitempty"`
	SID       string            `json:"sid,omitempty"`    // native session reference
	SIDSource string            `json:"sid_source,omitempty"`
	Cwd       string            `json:"cwd"`
	Argv      []string          `json:"argv,omitempty"` // foreground process argv
	Cmdline   string            `json:"cmdline,omitempty"`
	Shell     bool              `json:"shell,omitempty"` // no agent: comes back as a shell at cwd
	Env       map[string]string `json:"env,omitempty"`   // faithful live env (secrets; 0600)
}

// DefaultRoot is the canonical archive tree, independent of entrypoint
// (CLI or plugin action), so capture and resume always agree.
func DefaultRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".config", "herdr", "archives")
	}
	return filepath.Join(home, ".config", "herdr", "archives")
}

// Dir is the archive directory for a session.
func Dir(root, session string) string {
	if root == "" {
		root = DefaultRoot()
	}
	return filepath.Join(root, session)
}

// Save writes the snapshot with 0600 perms and repoints `last`.
func (s *Snapshot) Save(root string) (string, error) {
	dir := Dir(root, s.Session)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	name := fmt.Sprintf("herdr_%s.json", s.CreatedAt.Format("20060102T150405"))
	path := filepath.Join(dir, name)
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return "", err
	}
	last := filepath.Join(dir, "last")
	_ = os.Remove(last)
	if err := os.Symlink(filepath.Base(path), last); err != nil {
		return "", err
	}
	return path, nil
}

// Load reads a snapshot from a path.
func Load(path string) (*Snapshot, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s Snapshot
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if s.Version > Version {
		return nil, fmt.Errorf("%s: snapshot version %d newer than supported %d", path, s.Version, Version)
	}
	return &s, nil
}

// Latest resolves the `last` snapshot for a session, falling back to the
// newest timestamped file.
func Latest(root, session string) (string, error) {
	dir := Dir(root, session)
	if p, err := filepath.EvalSymlinks(filepath.Join(dir, "last")); err == nil {
		return p, nil
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "herdr_*.json"))
	if len(matches) == 0 {
		return "", fmt.Errorf("no snapshots in %s", dir)
	}
	return matches[len(matches)-1], nil
}

// AgentPanes flattens all captured panes that hold an agent.
func (s *Snapshot) AgentPanes() []Pane {
	var out []Pane
	for _, w := range s.Workspaces {
		for _, t := range w.Tabs {
			for _, p := range t.Panes {
				if p.Agent != "" {
					out = append(out, p)
				}
			}
		}
	}
	return out
}
