package resume

import (
	"testing"

	"github.com/abhirup-dev/herdr-resurrect/internal/manifest"
)

func TestDiffMapsPlannerEnvironmentDriftToAction(t *testing.T) {
	for _, tc := range []struct {
		name        string
		capturedEnv map[string]string
		liveEnv     map[string]string
		wantAction  Action
		wantReason  string
	}{
		{
			name:        "changed replayable value",
			capturedEnv: map[string]string{"PROJECT_MODE": "review"},
			liveEnv:     map[string]string{"PROJECT_MODE": "implementation"},
			wantAction:  Replace,
			wantReason:  "replay env changed: PROJECT_MODE",
		},
		{
			name:       "empty captured environment",
			liveEnv:    map[string]string{"ANTHROPIC_API_KEY": "live-only"},
			wantAction: KeepNative,
			wantReason: "live env matches manifest",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			capturedPane := manifest.Pane{
				Key: "agent", PaneID: "pane", Agent: "claude", Name: "agent", Env: tc.capturedEnv,
			}
			livePane := capturedPane
			livePane.Env = tc.liveEnv
			captured := &manifest.Snapshot{Workspaces: []manifest.Workspace{{Tabs: []manifest.Tab{{Panes: []manifest.Pane{capturedPane}}}}}}
			live := &manifest.Snapshot{Workspaces: []manifest.Workspace{{Tabs: []manifest.Tab{{Panes: []manifest.Pane{livePane}}}}}}

			plan := Diff(captured, live)
			if len(plan.Panes) != 1 {
				t.Fatalf("panes = %#v", plan.Panes)
			}
			if plan.Panes[0].Action != tc.wantAction || plan.Panes[0].Reason != tc.wantReason {
				t.Fatalf("action/reason = %s/%q, want %s/%q", plan.Panes[0].Action, plan.Panes[0].Reason, tc.wantAction, tc.wantReason)
			}
		})
	}
}
