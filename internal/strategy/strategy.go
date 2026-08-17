// Package strategy maps captured agent kinds to their native resume argv,
// resurrect-style. "~" (skip) and "->" (remap) conventions can grow here
// later; for now the table mirrors herdr's documented resume commands.
package strategy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SessionOnDisk reports whether the captured native session still exists on
// disk. A conversation with zero turns is never persisted, so its sid is
// dead regardless of provider: relaunching with --resume would just crash
// the agent back to a shell.
func SessionOnDisk(kind, sid string, env map[string]string) bool {
	if sid == "" {
		return false
	}
	home, _ := os.UserHomeDir()
	switch kind {
	case "claude", "grok":
		cfg := env["CLAUDE_CONFIG_DIR"]
		if cfg == "" {
			cfg = filepath.Join(home, ".claude")
		}
		m, _ := filepath.Glob(filepath.Join(cfg, "projects", "*", sid+".jsonl"))
		return len(m) > 0
	case "codex":
		m, _ := filepath.Glob(filepath.Join(home, ".codex", "sessions", "*", "*", "*", "*"+sid+"*"))
		return len(m) > 0
	case "pi":
		_, err := os.Stat(sid)
		return err == nil
	default:
		return true // assume resumable; verify report will say otherwise
	}
}

// ResumeArgs returns the argv tail that resumes an agent of the given kind
// with the captured native session id. ok=false means unknown kind: the
// relaunch keeps argv verbatim and the verify report flags it.
func ResumeArgs(kind, sid string) ([]string, bool) {
	if sid == "" {
		return nil, false
	}
	switch kind {
	case "claude", "grok":
		return []string{"--resume", sid}, true
	case "codex":
		return []string{"resume", sid}, true
	case "pi":
		return []string{"--session", sid}, true
	case "omp":
		return []string{"--resume=" + sid}, true
	case "copilot":
		return []string{"--resume=" + sid}, true
	case "kimi":
		return []string{"--session", sid}, true
	default:
		return nil, false
	}
}

// ProviderVar reports whether a var is provider-defining and a value change
// must force a pane replacement. Absent-vs-present is handled separately.
var providerPrefixes = []string{"ANTHROPIC_", "OPENAI_", "ZAI_", "GEMINI_", "DEEPC_", "KCLAUDE_", "REPLICATE_", "CODEX_"}
var providerSuffixes = []string{"_BASE_URL", "_AUTH_TOKEN", "_API_KEY", "_MODEL"}

func ProviderVar(k string) bool {
	for _, p := range providerPrefixes {
		if strings.HasPrefix(k, p) {
			return true
		}
	}
	for _, s := range providerSuffixes {
		if strings.HasSuffix(k, s) {
			return true
		}
	}
	return false
}

// TransientEnv lists vars that differ per pane incarnation by design and are
// never evidence of provider loss: herdr re-injects HERDR_* fresh, shells
// churn their session keys, and macOS adds per-process system vars.
func TransientEnv(k string) bool {
	for _, p := range []string{"HERDR_", "SSH_", "LC_", "XPC", "STARSHIP_", "ATUIN_", "__CF", "__MISE", "_ZO", "OSLog"} {
		if strings.HasPrefix(k, p) {
			return true
		}
	}
	switch k {
	case "SHLVL", "PWD", "OLDPWD", "_", "LINES", "COLUMNS", "TERM", "TERM_PROGRAM", "TERM_PROGRAM_VERSION", "TERM_SESSION_ID", "TMPDIR", "DISPLAY", "SSH_AUTH_SOCK", "COMMAND_MODE", "LaunchInstanceID", "OSLogRateLimit", "MISE_SHELL", "GHOSTTY_SURFACE_ID":
		return true
	}
	return false
}

// ReplayEnv filters a captured env block down to what should be blindly
// replayed into a new pane. HERDR_* is dropped: herdr injects fresh values.
func ReplayEnv(env map[string]string) map[string]string {
	out := make(map[string]string, len(env))
	for k, v := range env {
		if strings.HasPrefix(k, "HERDR_") {
			continue
		}
		out[k] = v
	}
	return out
}

// RelaunchCmdline composes the shell command for pane run. If argv already
// carries the resume fragment (the pane was captured while resuming), the
// fragment is not appended twice.
func RelaunchCmdline(argv []string, resumeArgs []string) string {
	parts := append([]string{}, argv...)
	if len(resumeArgs) > 0 {
		joined := strings.Join(parts, " ")
		sid := resumeArgs[len(resumeArgs)-1]
		if !strings.Contains(joined, sid) {
			parts = append(parts, resumeArgs...)
		}
	}
	return strings.Join(parts, " ")
}

// LaunchCmdline is RelaunchCmdline with fallbacks for processes whose argv
// is hidden from the process table (pi reports argv0 only): try herdr's
// cmdline, then the detected agent kind as the bare command.
func LaunchCmdline(argv []string, cmdline, kind string, resumeArgs []string) string {
	if len(argv) == 0 && cmdline != "" {
		argv = strings.Fields(cmdline)
	}
	if len(argv) == 0 && kind != "" {
		argv = []string{kind}
	}
	return RelaunchCmdline(argv, resumeArgs)
}

// Describe renders argv for one-line plans.
func Describe(argv []string) string {
	if len(argv) == 0 {
		return "(none)"
	}
	return fmt.Sprintf("%s…", strings.Join(argv[:min(3, len(argv))], " "))
}

// ProviderLabel derives a short human label ("z.ai", "local proxy",
// "anthropic") from a captured env block — the base URL the agent actually
// talked to, not any launcher knowledge.
func ProviderLabel(kind string, env map[string]string) string {
	// kind-specific only for known kinds: a codex pane inherits the user's
	// shell vars (ANTHROPIC_*, KCLAUDE_*…) but only its own base URL
	// describes it. Unknown kinds fall back to any provider-looking var.
	var keys []string
	switch kind {
	case "claude", "grok":
		keys = []string{"ANTHROPIC_BASE_URL"}
	case "codex":
		keys = []string{"OPENAI_BASE_URL", "OPENAI_API_BASE"}
	default:
		keys = []string{"ANTHROPIC_BASE_URL", "OPENAI_BASE_URL", "KCLAUDE_BASE_URL", "DEEPC_BASE_URL", "ZAI_BASE_URL"}
	}
	for _, k := range keys {
		if u := env[k]; u != "" {
			host := u
			if i := strings.Index(host, "://"); i >= 0 {
				host = host[i+3:]
			}
			if i := strings.IndexByte(host, '/'); i >= 0 {
				host = host[:i]
			}
			switch {
			case strings.HasPrefix(host, "127.0.0.1"), caseLocal(host):
				return "local proxy"
			case strings.Contains(host, "z.ai"):
				return "z.ai"
			case strings.Contains(host, "anthropic.com"):
				return "anthropic"
			default:
				return host
			}
		}
	}
	return kind
}

func caseLocal(h string) bool { return strings.HasPrefix(h, "localhost") }

// TranscriptSize returns the on-disk size of an agent's native transcript,
// so snapshots can show what a restore actually carries back.
func TranscriptSize(kind, sid string, env map[string]string) int64 {
	if sid == "" {
		return 0
	}
	home, _ := os.UserHomeDir()
	var matches []string
	switch kind {
	case "claude", "grok":
		cfg := env["CLAUDE_CONFIG_DIR"]
		if cfg == "" {
			cfg = filepath.Join(home, ".claude")
		}
		matches, _ = filepath.Glob(filepath.Join(cfg, "projects", "*", sid+".jsonl"))
	case "codex":
		matches, _ = filepath.Glob(filepath.Join(home, ".codex", "sessions", "*", "*", "*", "*"+sid+"*"))
	case "pi":
		if fi, err := os.Stat(sid); err == nil {
			return fi.Size()
		}
	}
	var total int64
	for _, m := range matches {
		if fi, err := os.Stat(m); err == nil {
			total += fi.Size()
		}
	}
	return total
}
