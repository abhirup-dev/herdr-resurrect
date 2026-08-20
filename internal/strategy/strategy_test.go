package strategy

import "testing"

func TestReplayEnvDropsStaleRuntimeButKeepsProviderConfig(t *testing.T) {
	captured := map[string]string{
		"ANTHROPIC_BASE_URL":   "https://api.z.ai/api/anthropic",
		"ANTHROPIC_AUTH_TOKEN": "test-provider-token",
		"PATH":                 "/opt/homebrew/bin:/usr/bin",
		"CMUX_SURFACE_ID":      "source-surface",
		"HERDR_PANE_ID":        "w1:p1",
		"CLAUDECODE":           "1",
		"CLAUDE_PID":           "12345",
		"NODE_OPTIONS":         "--require /var/folders/test/T/cmux-claude-node-options/restore-node-options.cjs",
	}

	got := ReplayEnv(captured)
	for key, want := range map[string]string{
		"ANTHROPIC_BASE_URL":   captured["ANTHROPIC_BASE_URL"],
		"ANTHROPIC_AUTH_TOKEN": captured["ANTHROPIC_AUTH_TOKEN"],
		"PATH":                 captured["PATH"],
	} {
		if got[key] != want {
			t.Errorf("ReplayEnv()[%q] = %q, want %q", key, got[key], want)
		}
	}
	for _, key := range []string{"CMUX_SURFACE_ID", "HERDR_PANE_ID", "CLAUDECODE", "CLAUDE_PID", "NODE_OPTIONS"} {
		if _, ok := got[key]; ok {
			t.Errorf("ReplayEnv() unexpectedly retained %q", key)
		}
	}
}

func TestReplayEnvKeepsUserNodeOptions(t *testing.T) {
	captured := map[string]string{"NODE_OPTIONS": "--max-old-space-size=4096"}
	if got := ReplayEnv(captured)["NODE_OPTIONS"]; got != captured["NODE_OPTIONS"] {
		t.Fatalf("ReplayEnv() NODE_OPTIONS = %q, want ordinary user setting preserved", got)
	}
}

func TestLaunchCmdlinePreservesResolvedLauncherArguments(t *testing.T) {
	for _, tc := range []struct {
		name string
		argv []string
		want string
	}{
		{
			name: "native opus one million",
			argv: []string{"claude", "--model", "opus[1m]", "--effort", "medium"},
			want: "claude --model 'opus[1m]' --effort medium --resume session-id",
		},
		{
			name: "spaces and quotes",
			argv: []string{"claude", "--settings", "agent's local settings.json"},
			want: "claude --settings 'agent'\"'\"'s local settings.json' --resume session-id",
		},
		{
			name: "GLM mapped through opus alias",
			argv: []string{"claude", "--enable-auto-mode", "--model", "opus", "--effort", "high"},
			want: "claude --enable-auto-mode --model opus --effort high --resume session-id",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := LaunchCmdline(tc.argv, "", "claude", []string{"--resume", "session-id"}); got != tc.want {
				t.Errorf("LaunchCmdline() = %q, want %q", got, tc.want)
			}
		})
	}
}
