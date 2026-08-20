package resume

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/abhirup-dev/herdr-resurrect/internal/manifest"
	"github.com/abhirup-dev/herdr-resurrect/internal/planner"
)

func TestLaunchPathsResolveTakenAgentName(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(manifest.Pane) error
	}{
		{
			name: "legacy additive launch",
			run: func(pane manifest.Pane) error {
				_, err := launchPane("test", "anchor", PanePlan{Manifest: pane, Fresh: true}, "claude")
				return err
			},
		},
		{
			name: "compiled additive launch",
			run: func(pane manifest.Pane) error {
				return launchInPane("test", "existing-pane", pane)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logPath := installFakeHerdr(t, fakeHerdrOptions{})
			output := captureStdout(t, func() {
				if err := tc.run(manifest.Pane{
					Key: "captured", Agent: "claude", Name: "taken", Cwd: t.TempDir(), Argv: []string{"claude"},
				}); err != nil {
					t.Fatalf("launch: %v", err)
				}
			})
			log, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatal(err)
			}
			if got, want := strings.Fields(string(log)), []string{"taken", "taken-1"}; strings.Join(got, ",") != strings.Join(want, ",") {
				t.Fatalf("rename candidates = %v, want %v", got, want)
			}
			if !strings.Contains(output, "name taken, restored as taken-1") {
				t.Fatalf("missing collision notice in output %q", output)
			}
		})
	}
}

func TestAttachNameStopsAfterSixCandidates(t *testing.T) {
	logPath := installFakeHerdr(t, fakeHerdrOptions{failAllRenames: true})
	err := attachName("test", "pane", "taken")
	if err == nil {
		t.Fatal("rename unexpectedly succeeded")
	}
	log, readErr := os.ReadFile(logPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if got, want := len(strings.Fields(string(log))), 6; got != want {
		t.Fatalf("rename attempts = %d, want %d", got, want)
	}
}

func TestApplyAdditiveOperationLaunchesInVacatedDestinationPane(t *testing.T) {
	logPath := installFakeHerdr(t, fakeHerdrOptions{logWholeCommand: true})
	state := &additiveState{
		workspaceIDs:      map[string]string{"work": "w1"},
		tabIDs:            map[string]string{destinationKey("work", "implementation"): "w1:t1"},
		fallbacks:         map[string]string{destinationKey("work", "implementation"): "w1:p1"},
		paneIDs:           map[string]string{},
		liveNames:         map[string]bool{},
		createdWorkspaces: map[string]bool{},
		createdTabs:       map[string]bool{},
		paneDestinations:  map[string]string{"w1:p1": destinationKey("work", "implementation")},
		emptyPanes:        map[string]bool{"w1:p1": true},
	}
	err := applyAdditiveOperation("work", state, planner.Operation{
		WorkspaceKey: "work", WorkspaceID: "w1",
		TabKey: "implementation", TabID: "w1:t1",
		DestinationPaneID: "w1:p1",
		Pane:              manifest.Pane{Key: "agent", PaneID: "captured", Agent: "claude", Name: "agent", Argv: []string{"claude"}},
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	commands := string(log)
	if strings.Contains(commands, "pane split") {
		t.Fatalf("vacated destination was split instead of reused:\n%s", commands)
	}
	if !strings.Contains(commands, "pane run w1:p1") {
		t.Fatalf("agent was not launched in vacated pane:\n%s", commands)
	}
	if state.emptyPanes["w1:p1"] {
		t.Fatal("reused pane remained marked empty")
	}
}

type fakeHerdrOptions struct {
	logWholeCommand bool
	failAllRenames  bool
}

func installFakeHerdr(t *testing.T, options fakeHerdrOptions) string {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "commands")
	script := filepath.Join(dir, "herdr")
	commandLog := ""
	if options.logWholeCommand {
		commandLog = `printf '%s\n' "$*" >>"$FAKE_HERDR_LOG"`
	}
	rename := `printf '%s\n' "$last" >>"$FAKE_HERDR_LOG"; [ "$last" = taken ] && exit 1; printf '{"result":{}}'`
	if options.logWholeCommand {
		rename = `printf '{"result":{}}'`
	} else if options.failAllRenames {
		rename = `printf '%s\n' "$last" >>"$FAKE_HERDR_LOG"; exit 1`
	}
	content := `#!/bin/sh
last=""
for arg in "$@"; do last="$arg"; done
` + commandLog + `
case " $* " in
  *" pane split "*) printf '{"result":{"pane":{"pane_id":"new-pane"}}}' ;;
  *" pane run "*) printf '{"result":{}}' ;;
  *" agent get "*) printf '{"result":{"agent":{"agent":"claude"}}}' ;;
  *" agent rename "*) ` + rename + ` ;;
  *) printf '{"result":{}}' ;;
esac
`
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDR_BIN_PATH", script)
	t.Setenv("FAKE_HERDR_LOG", logPath)
	return logPath
}

func captureStdout(t *testing.T, run func()) string {
	t.Helper()
	old := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	defer func() { os.Stdout = old }()
	run()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return string(output)
}
