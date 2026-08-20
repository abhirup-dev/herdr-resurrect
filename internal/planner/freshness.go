package planner

import (
	"math"
	"reflect"

	"github.com/abhirup-dev/herdr-resurrect/internal/manifest"
	"github.com/abhirup-dev/herdr-resurrect/internal/strategy"
)

// ExactWorkspaceMatch reports whether a captured workspace is represented by
// the same live tabs, pane identities, ordering, split geometry, cwd, and
// provider configuration. Volatile focus, state, terminal titles, and native
// session ids do not affect freshness.
func ExactWorkspaceMatch(captured manifest.Workspace, live *manifest.Snapshot) bool {
	if live == nil {
		return false
	}
	matched := -1
	for index, workspace := range live.Workspaces {
		if !sameWorkspaceIdentity(captured, workspace) {
			continue
		}
		if matched >= 0 {
			return false
		}
		matched = index
	}
	if matched < 0 {
		return false
	}
	current := live.Workspaces[matched]
	if len(captured.Tabs) != len(current.Tabs) {
		return false
	}
	for index := range captured.Tabs {
		if !exactTabMatch(captured.Tabs[index], current.Tabs[index]) {
			return false
		}
	}
	return true
}

// ExactSnapshotMatch reports whether a full snapshot still describes every
// replay-relevant part of the live session. Capture metadata and volatile UI
// state are ignored; scoped snapshots can never represent a complete session.
func ExactSnapshotMatch(captured, live *manifest.Snapshot) bool {
	if captured == nil || live == nil || captured.CaptureScope != nil || live.CaptureScope != nil ||
		captured.Session != live.Session || len(captured.Workspaces) != len(live.Workspaces) {
		return false
	}
	for workspaceIndex := range captured.Workspaces {
		left, right := captured.Workspaces[workspaceIndex], live.Workspaces[workspaceIndex]
		if left.ID != right.ID || left.Label != right.Label || left.Cwd != right.Cwd || len(left.Tabs) != len(right.Tabs) {
			return false
		}
		for tabIndex := range left.Tabs {
			leftTab, rightTab := left.Tabs[tabIndex], right.Tabs[tabIndex]
			if leftTab.ID != rightTab.ID || leftTab.Label != rightTab.Label || len(leftTab.Panes) != len(rightTab.Panes) ||
				!exactSnapshotLayoutMatch(leftTab.Layout, rightTab.Layout, len(leftTab.Panes)) {
				return false
			}
			for paneIndex := range leftTab.Panes {
				if !exactSnapshotPaneMatch(leftTab.Panes[paneIndex], rightTab.Panes[paneIndex]) {
					return false
				}
			}
		}
	}
	return true
}

func exactSnapshotPaneMatch(left, right manifest.Pane) bool {
	return left.Key == right.Key && left.PaneID == right.PaneID && left.Agent == right.Agent && left.Name == right.Name &&
		left.SID == right.SID && left.SIDSource == right.SIDSource && left.Cwd == right.Cwd && left.Shell == right.Shell &&
		left.Cmdline == right.Cmdline && reflect.DeepEqual(left.Argv, right.Argv) &&
		reflect.DeepEqual(strategy.ReplayEnv(left.Env), strategy.ReplayEnv(right.Env))
}

func exactSnapshotLayoutMatch(left, right *manifest.Layout, paneCount int) bool {
	if paneCount <= 1 {
		return true
	}
	if left == nil || right == nil || left.Zoomed != right.Zoomed {
		return false
	}
	return exactLayoutMatch(left, right, paneCount)
}

func sameWorkspaceIdentity(captured, live manifest.Workspace) bool {
	if captured.Label != "" {
		return captured.Label == live.Label
	}
	return captured.ID != "" && captured.ID == live.ID
}

func exactTabMatch(captured, live manifest.Tab) bool {
	if captured.Label != live.Label || len(captured.Panes) != len(live.Panes) {
		return false
	}
	for index := range captured.Panes {
		if !exactPaneMatch(captured.Panes[index], live.Panes[index]) {
			return false
		}
	}
	return exactLayoutMatch(captured.Layout, live.Layout, len(captured.Panes))
}

func exactPaneMatch(captured, live manifest.Pane) bool {
	if captured.Agent != live.Agent || captured.Cwd != live.Cwd {
		return false
	}
	if captured.Agent != "" {
		if captured.Name != "" {
			if captured.Name != live.Name {
				return false
			}
		} else if captured.Key != live.Key {
			return false
		}
	}
	return reflect.DeepEqual(providerEnvironment(captured.Env), providerEnvironment(live.Env))
}

func providerEnvironment(env map[string]string) map[string]string {
	provider := map[string]string{}
	for key, value := range env {
		if strategy.ProviderVar(key) {
			provider[key] = value
		}
	}
	return provider
}

func exactLayoutMatch(captured, live *manifest.Layout, paneCount int) bool {
	if paneCount <= 1 {
		return true
	}
	if captured == nil || live == nil || len(captured.Splits) != len(live.Splits) {
		return false
	}
	for index := range captured.Splits {
		left, right := captured.Splits[index], live.Splits[index]
		if normalizedDirection(left.Direction) != normalizedDirection(right.Direction) ||
			math.Abs(normalizedRatio(left.Ratio)-normalizedRatio(right.Ratio)) > 0.02 {
			return false
		}
	}
	return true
}
