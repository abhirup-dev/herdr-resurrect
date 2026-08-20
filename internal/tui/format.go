package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/abhirup-dev/herdr-resurrect/internal/manifest"
	"github.com/abhirup-dev/herdr-resurrect/internal/resume"
	"github.com/abhirup-dev/herdr-resurrect/internal/strategy"
)

func relTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func snapshotWhen(t time.Time) string {
	now := time.Now()
	today := now.Year() == t.Year() && now.YearDay() == t.YearDay()
	yesterday := now.AddDate(0, 0, -1)
	switch {
	case today:
		return "Today " + t.Format("15:04")
	case yesterday.Year() == t.Year() && yesterday.YearDay() == t.YearDay():
		return "Yesterday " + t.Format("15:04")
	case now.Year() == t.Year():
		return t.Format("Jan 2, 15:04")
	default:
		return t.Format("2006-01-02 15:04")
	}
}

func snapshotName(snapshot *manifest.Snapshot) string {
	if snapshot == nil {
		return "unnamed capture"
	}
	if name := strings.TrimSpace(snapshot.Name); name != "" {
		return name
	}
	return snapshotWhen(snapshot.CreatedAt.Local())
}

func humanBytes(n int64) string {
	switch {
	case n == 0:
		return "—"
	case n < 1024:
		return fmt.Sprintf("%dB", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1fKB", float64(n)/1024)
	case n < 1024*1024*1024:
		return fmt.Sprintf("%.1fMB", float64(n)/(1024*1024))
	default:
		return fmt.Sprintf("%.1fGB", float64(n)/(1024*1024*1024))
	}
}

func snapshotStats(s *manifest.Snapshot) (agents string, size int64, n int) {
	if s == nil {
		return "—", 0, 0
	}
	var names []string
	for _, p := range s.AgentPanes() {
		names = append(names, p.Key)
		size += strategy.TranscriptSize(p.Agent, p.SID, p.Env)
	}
	if len(names) > 4 {
		agents = strings.Join(names[:4], " ") + " …"
	} else {
		agents = strings.Join(names, " ")
	}
	if agents == "" {
		agents = "shells only"
	}
	return agents, size, len(names)
}

// agentRoster renders the fleet with per-kind brand icons before each name.
func agentRoster(s *manifest.Snapshot) string {
	if s == nil {
		return "—"
	}
	var names []string
	for _, pane := range s.AgentPanes() {
		names = append(names, icon(pane.Agent, pane.Title)+" "+pane.Key)
	}
	if len(names) > 4 {
		return strings.Join(names[:4], "  ") + "  …"
	}
	if len(names) == 0 {
		return "shells only"
	}
	return strings.Join(names, "  ")
}

func plural(n int, s string) string {
	if n == 1 {
		return s
	}
	return s + "s"
}

func clipLabel(s string, width int) string {
	runes := []rune(s)
	if len(runes) <= width {
		return s
	}
	if width <= 1 {
		return string(runes[:width])
	}
	return string(runes[:width-1]) + "…"
}

func actionLabel(action resume.Action) string {
	switch action {
	case resume.KeepNative:
		return "UNCHANGED"
	case resume.Replace:
		return "RESTART"
	case resume.Relaunch:
		return "START FRESH"
	case resume.Resurrect:
		return "RESTORE"
	case resume.ShellKeep:
		return "PRESERVE"
	default:
		return string(action)
	}
}

func workspaceKey(workspace manifest.Workspace) string {
	if workspace.Label != "" {
		return workspace.Label
	}
	return workspace.ID
}

func tabKey(tab manifest.Tab) string {
	if tab.Label != "" {
		return tab.Label
	}
	return tab.ID
}

func layoutLabel(tab manifest.Tab) string {
	count := len(tab.Panes)
	base := fmt.Sprintf("%d %s", count, plural(count, "pane"))
	if count <= 1 || tab.Layout == nil || len(tab.Layout.Splits) == 0 {
		return base
	}
	direction := tab.Layout.Splits[0].Direction
	for _, split := range tab.Layout.Splits[1:] {
		if split.Direction != direction {
			return base + " · mixed"
		}
	}
	switch direction {
	case "right":
		return base + " · horizontal"
	case "down":
		return base + " · vertical"
	default:
		return base
	}
}

func filteredLayoutLabel(tab manifest.Tab, selected map[string]bool) string {
	count := 0
	for _, pane := range tab.Panes {
		if selected[pane.Key] {
			count++
		}
	}
	if count == len(tab.Panes) {
		return layoutLabel(tab)
	}
	base := fmt.Sprintf("%d %s", count, plural(count, "pane"))
	if count > 1 {
		base += " · compacted"
	}
	return base
}

func providerDisplay(kind string, env map[string]string) string {
	model := strings.ToLower(env["ANTHROPIC_DEFAULT_OPUS_MODEL"] + " " + env["ANTHROPIC_MODEL"])
	switch {
	case strings.Contains(model, "gpt"):
		return "GPT"
	case strings.Contains(model, "glm"):
		return "GLM"
	case kind == "claude":
		return "Claude"
	case kind == "":
		return "shell"
	default:
		return strings.ToUpper(kind[:1]) + kind[1:]
	}
}

func fitLine(line string, width int) string {
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(line) <= width {
		return line
	}
	return ansi.Truncate(line, width, "…")
}

func fitLines(lines []string, width int) []string {
	fitted := make([]string, len(lines))
	for index, line := range lines {
		fitted[index] = fitLine(line, width)
	}
	return fitted
}

func fitBlock(block string, width int) string {
	return strings.Join(fitLines(strings.Split(block, "\n"), width), "\n")
}

func joinColumns(left, right []string, leftWidth, rightWidth int) string {
	left = fitLines(left, leftWidth)
	right = fitLines(right, rightWidth)
	height := max(len(left), len(right))
	var out strings.Builder
	for i := range height {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		padding := max(0, leftWidth-ansi.StringWidth(l))
		out.WriteString(l)
		out.WriteString(strings.Repeat(" ", padding))
		out.WriteString(styDim.Render(" │ "))
		out.WriteString(r)
		if i+1 < height {
			out.WriteByte('\n')
		}
	}
	return out.String()
}
