package planner

import (
	"math"
	"sort"

	"github.com/abhirup-dev/herdr-resurrect/internal/manifest"
)

type layoutTree struct {
	pane      *manifest.Pane
	direction string
	ratio     float64
	first     *layoutTree
	second    *layoutTree
}

type geometryPane struct {
	pane manifest.Pane
	rect manifest.Rect
}

func layoutPlacements(tab manifest.Tab, included Selection, mode GeometryMode) ([]string, map[string]Placement) {
	root := filteredLayoutTree(tab, included)
	placements := map[string]Placement{}
	if root == nil {
		return nil, placements
	}
	first := firstLeaf(root)
	order := []string{first}
	placements[first] = Placement{Mode: mode, Direction: "right", Ratio: 0.5}
	seen := map[string]bool{first: true}
	var schedule func(*layoutTree, string)
	schedule = func(node *layoutTree, seed string) {
		if node == nil || node.pane != nil {
			return
		}
		second := firstLeaf(node.second)
		if !seen[second] {
			placements[second] = Placement{
				Mode:      mode,
				AnchorKey: seed,
				Direction: normalizedDirection(node.direction),
				Ratio:     normalizedRatio(node.ratio),
			}
			order = append(order, second)
			seen[second] = true
		}
		schedule(node.first, seed)
		schedule(node.second, second)
	}
	schedule(root, first)
	return order, placements
}

func filteredLayoutTree(tab manifest.Tab, included Selection) *layoutTree {
	root := capturedLayoutTree(tab)
	return pruneLayoutTree(root, included)
}

func capturedLayoutTree(tab manifest.Tab) *layoutTree {
	if len(tab.Panes) == 0 {
		return nil
	}
	if tab.Layout == nil {
		return fallbackLayoutTree(tab)
	}
	paneByID := map[string]manifest.Pane{}
	for _, pane := range tab.Panes {
		paneByID[pane.PaneID] = pane
	}
	var panes []geometryPane
	for _, layoutPane := range tab.Layout.Panes {
		pane, ok := paneByID[layoutPane.PaneID]
		if !ok {
			continue
		}
		panes = append(panes, geometryPane{pane: pane, rect: layoutPane.Rect})
	}
	if len(panes) != len(tab.Panes) {
		return fallbackLayoutTree(tab)
	}
	root := buildLayoutTree(panes, tab.Layout.Splits)
	if root == nil {
		return fallbackLayoutTree(tab)
	}
	return root
}

func buildLayoutTree(panes []geometryPane, splits []manifest.Split) *layoutTree {
	if len(panes) == 0 {
		return nil
	}
	if len(panes) == 1 {
		pane := panes[0].pane
		return &layoutTree{pane: &pane}
	}
	bounds := unionRects(panes)
	for _, split := range splits {
		if !sameRect(split.Rect, bounds) {
			continue
		}
		first, second := partitionGeometry(panes, split)
		if len(first) == 0 || len(second) == 0 {
			continue
		}
		firstTree := buildLayoutTree(first, splits)
		secondTree := buildLayoutTree(second, splits)
		if firstTree == nil || secondTree == nil {
			return nil
		}
		return &layoutTree{
			direction: normalizedDirection(split.Direction),
			ratio:     normalizedRatio(split.Ratio),
			first:     firstTree,
			second:    secondTree,
		}
	}
	return nil
}

func partitionGeometry(panes []geometryPane, split manifest.Split) (first, second []geometryPane) {
	if split.Direction == "down" {
		cut := float64(split.Rect.Y) + float64(split.Rect.Height)*normalizedRatio(split.Ratio)
		for _, pane := range panes {
			center := float64(pane.rect.Y) + float64(pane.rect.Height)/2
			if center < cut {
				first = append(first, pane)
			} else {
				second = append(second, pane)
			}
		}
		return
	}
	cut := float64(split.Rect.X) + float64(split.Rect.Width)*normalizedRatio(split.Ratio)
	for _, pane := range panes {
		center := float64(pane.rect.X) + float64(pane.rect.Width)/2
		if center < cut {
			first = append(first, pane)
		} else {
			second = append(second, pane)
		}
	}
	return
}

func fallbackLayoutTree(tab manifest.Tab) *layoutTree {
	var leaves []*layoutTree
	for index := range tab.Panes {
		pane := tab.Panes[index]
		leaves = append(leaves, &layoutTree{pane: &pane})
	}
	if len(leaves) == 0 {
		return nil
	}
	root := leaves[len(leaves)-1]
	for index := len(leaves) - 2; index >= 0; index-- {
		direction, ratio := "right", 0.5
		splitIndex := min(index, len(layoutSplits(tab))-1)
		if splitIndex >= 0 {
			split := layoutSplits(tab)[splitIndex]
			direction, ratio = normalizedDirection(split.Direction), normalizedRatio(split.Ratio)
		}
		root = &layoutTree{direction: direction, ratio: ratio, first: leaves[index], second: root}
	}
	return root
}

func pruneLayoutTree(node *layoutTree, included Selection) *layoutTree {
	if node == nil {
		return nil
	}
	if node.pane != nil {
		if included[node.pane.Key] {
			pane := *node.pane
			return &layoutTree{pane: &pane}
		}
		return nil
	}
	first := pruneLayoutTree(node.first, included)
	second := pruneLayoutTree(node.second, included)
	switch {
	case first == nil:
		return second
	case second == nil:
		return first
	default:
		return &layoutTree{direction: node.direction, ratio: node.ratio, first: first, second: second}
	}
}

func firstLeaf(node *layoutTree) string {
	for node != nil && node.pane == nil {
		node = node.first
	}
	if node == nil || node.pane == nil {
		return ""
	}
	return node.pane.Key
}

func unionRects(panes []geometryPane) manifest.Rect {
	minX, minY := panes[0].rect.X, panes[0].rect.Y
	maxX := panes[0].rect.X + panes[0].rect.Width
	maxY := panes[0].rect.Y + panes[0].rect.Height
	for _, pane := range panes[1:] {
		minX = min(minX, pane.rect.X)
		minY = min(minY, pane.rect.Y)
		maxX = max(maxX, pane.rect.X+pane.rect.Width)
		maxY = max(maxY, pane.rect.Y+pane.rect.Height)
	}
	return manifest.Rect{X: minX, Y: minY, Width: maxX - minX, Height: maxY - minY}
}

func sameRect(left, right manifest.Rect) bool {
	return left.X == right.X && left.Y == right.Y && left.Width == right.Width && left.Height == right.Height
}

func normalizedDirection(direction string) string {
	if direction == "down" {
		return "down"
	}
	return "right"
}

func normalizedRatio(ratio float64) float64 {
	if ratio <= 0 || ratio >= 1 || math.IsNaN(ratio) {
		return 0.5
	}
	return ratio
}

// layoutSplits returns a stable copy used by the fallback builder.
func layoutSplits(tab manifest.Tab) []manifest.Split {
	if tab.Layout == nil {
		return nil
	}
	splits := append([]manifest.Split(nil), tab.Layout.Splits...)
	sort.SliceStable(splits, func(i, j int) bool {
		left, right := splits[i].Rect, splits[j].Rect
		return left.Width*left.Height > right.Width*right.Height
	})
	return splits
}
