package planner

import (
	"slices"
	"testing"

	"github.com/abhirup-dev/herdr-resurrect/internal/manifest"
)

func TestLayoutPlacementsFallBackWhenInteriorSplitIsMalformed(t *testing.T) {
	tab := manifest.Tab{
		Panes: []manifest.Pane{
			{Key: "one", PaneID: "p1"},
			{Key: "two", PaneID: "p2"},
			{Key: "three", PaneID: "p3"},
		},
		Layout: &manifest.Layout{
			Panes: []manifest.LayoutPane{
				{PaneID: "p1", Rect: manifest.Rect{X: 0, Y: 0, Width: 30, Height: 100}},
				{PaneID: "p2", Rect: manifest.Rect{X: 30, Y: 0, Width: 30, Height: 100}},
				{PaneID: "p3", Rect: manifest.Rect{X: 60, Y: 0, Width: 30, Height: 100}},
			},
			Splits: []manifest.Split{
				{Direction: "right", Ratio: 1.0 / 3.0, Rect: manifest.Rect{X: 0, Y: 0, Width: 90, Height: 100}},
				// The interior split should cover x=30..90. Its malformed bounds
				// previously produced a tree with a nil child and dropped two panes.
				{Direction: "right", Ratio: 0.5, Rect: manifest.Rect{X: 31, Y: 0, Width: 59, Height: 100}},
			},
		},
	}
	included := Selection{"one": true, "two": true, "three": true}

	order, placements := layoutPlacements(tab, included, GeometryExact)
	if want := []string{"one", "two", "three"}; !slices.Equal(order, want) {
		t.Fatalf("placement order = %v, want fallback order %v", order, want)
	}
	if len(placements) != 3 {
		t.Fatalf("placements = %#v, want every selected pane", placements)
	}
}
