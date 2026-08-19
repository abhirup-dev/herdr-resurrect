package planner

import (
	"math"
	"reflect"

	"github.com/abhirupdas/herdr-archive/internal/manifest"
	"github.com/abhirupdas/herdr-archive/internal/strategy"
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
