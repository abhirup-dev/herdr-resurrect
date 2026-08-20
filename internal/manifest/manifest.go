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
const Version = 2

// Snapshot is one capture of a herdr session.
type Snapshot struct {
	Version      int           `json:"version"`
	Name         string        `json:"name,omitempty"`
	CreatedAt    time.Time     `json:"created_at"`
	Session      string        `json:"session"` // "default" or a named session
	SessionDir   string        `json:"session_dir,omitempty"`
	CaptureScope *CaptureScope `json:"capture_scope,omitempty"`
	Workspaces   []Workspace   `json:"workspaces"`
}

// CaptureScope marks the panes whose launch payload was captured. The full
// topology remains in the snapshot as geometry context; absent means every
// pane is captured, preserving version 1 behavior.
type CaptureScope struct {
	Panes []PaneRef `json:"panes"`
}

type PaneRef struct {
	WorkspaceID string `json:"workspace_id"`
	TabID       string `json:"tab_id"`
	PaneKey     string `json:"pane_key"`
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
	ID     string  `json:"id"`
	Label  string  `json:"label,omitempty"`
	Panes  []Pane  `json:"panes"`
	Layout *Layout `json:"layout,omitempty"`
}

type Layout struct {
	Area          Rect         `json:"area"`
	FocusedPaneID string       `json:"focused_pane_id,omitempty"`
	Panes         []LayoutPane `json:"panes,omitempty"`
	Splits        []Split      `json:"splits,omitempty"`
	Zoomed        bool         `json:"zoomed,omitempty"`
}

type Rect struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

type LayoutPane struct {
	PaneID  string `json:"pane_id"`
	Focused bool   `json:"focused,omitempty"`
	Rect    Rect   `json:"rect"`
}

type Split struct {
	ID        string  `json:"id"`
	Direction string  `json:"direction"`
	Ratio     float64 `json:"ratio"`
	Rect      Rect    `json:"rect"`
}

// Pane is a captured pane. Key is the stable identity used across restores:
// agent name when present, else the pane id. Pane IDs are never reused by
// herdr and are informational only.
type Pane struct {
	Key       string            `json:"key"`
	PaneID    string            `json:"pane_id"`         // informational, never identity
	Agent     string            `json:"agent,omitempty"` // detected kind (claude, codex, pi...)
	Name      string            `json:"name,omitempty"`  // herdr agent name
	State     string            `json:"state,omitempty"`
	Title     string            `json:"title,omitempty"` // terminal title at capture (icon source)
	SID       string            `json:"sid,omitempty"`   // native session reference
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

// CapturedPaneKeys returns the payload-bearing pane keys for one workspace.
// A nil map means the snapshot is unscoped and every pane is captured.
func (s *Snapshot) CapturedPaneKeys(workspaceID string) map[string]bool {
	if s == nil || s.CaptureScope == nil {
		return nil
	}
	keys := map[string]bool{}
	for _, ref := range s.CaptureScope.Panes {
		if ref.WorkspaceID == workspaceID {
			keys[ref.PaneKey] = true
		}
	}
	return keys
}

// CapturesPane reports whether a pane has a restorable payload rather than
// serving only as topology and geometry context.
func (s *Snapshot) CapturesPane(workspaceID, tabID, paneKey string) bool {
	if s == nil || s.CaptureScope == nil {
		return true
	}
	for _, ref := range s.CaptureScope.Panes {
		if ref.WorkspaceID == workspaceID && ref.TabID == tabID && ref.PaneKey == paneKey {
			return true
		}
	}
	return false
}

// CapturedPaneCount reports payload panes and total topology panes.
func (s *Snapshot) CapturedPaneCount() (captured, total int) {
	for _, w := range s.Workspaces {
		for _, t := range w.Tabs {
			for _, p := range t.Panes {
				total++
				if s.CapturesPane(w.ID, t.ID, p.Key) {
					captured++
				}
			}
		}
	}
	return
}

// AgentPanes flattens captured payload panes that hold an agent.
func (s *Snapshot) AgentPanes() []Pane {
	var out []Pane
	for _, w := range s.Workspaces {
		for _, t := range w.Tabs {
			for _, p := range t.Panes {
				if p.Agent != "" && s.CapturesPane(w.ID, t.ID, p.Key) {
					out = append(out, p)
				}
			}
		}
	}
	return out
}

// Filter returns the sub-snapshot matching the selectors. Empty selectors
// keep everything. Scoped snapshots retain their full topology and narrow the
// persisted capture scope so geometry context cannot become restorable.
func (s *Snapshot) Filter(wsID, tabID string, agents []string) *Snapshot {
	want := func(p Pane) bool {
		if len(agents) == 0 {
			return true
		}
		for _, a := range agents {
			if p.Key == a || p.Name == a || p.PaneID == a {
				return true
			}
		}
		return false
	}
	if s.CaptureScope != nil {
		copy := *s
		copy.Workspaces = append([]Workspace(nil), s.Workspaces...)
		copy.CaptureScope = &CaptureScope{}
		for _, w := range s.Workspaces {
			if wsID != "" && w.ID != wsID && w.Label != wsID {
				continue
			}
			for _, t := range w.Tabs {
				if tabID != "" && t.ID != tabID && t.Label != tabID {
					continue
				}
				for _, p := range t.Panes {
					if s.CapturesPane(w.ID, t.ID, p.Key) && want(p) {
						copy.CaptureScope.Panes = append(copy.CaptureScope.Panes, PaneRef{
							WorkspaceID: w.ID,
							TabID:       t.ID,
							PaneKey:     p.Key,
						})
					}
				}
			}
		}
		return &copy
	}

	out := &Snapshot{Version: s.Version, Name: s.Name, CreatedAt: s.CreatedAt, Session: s.Session, SessionDir: s.SessionDir}
	for _, w := range s.Workspaces {
		if wsID != "" && w.ID != wsID && w.Label != wsID {
			continue
		}
		mw := Workspace{ID: w.ID, Label: w.Label, Cwd: w.Cwd}
		for _, t := range w.Tabs {
			if tabID != "" && t.ID != tabID && t.Label != tabID {
				continue
			}
			mt := Tab{ID: t.ID, Label: t.Label, Layout: t.Layout}
			for _, p := range t.Panes {
				if want(p) {
					mt.Panes = append(mt.Panes, p)
				}
			}
			if len(mt.Panes) > 0 {
				mw.Tabs = append(mw.Tabs, mt)
			}
		}
		if len(mw.Tabs) > 0 {
			out.Workspaces = append(out.Workspaces, mw)
		}
	}
	return out
}

// DefaultName returns the human-readable date and time used when a capture
// is saved without an explicit name.
func DefaultName(t time.Time) string {
	return t.Local().Format("Jan 2, 2006 15:04")
}
