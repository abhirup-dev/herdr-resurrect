package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

type treeNode struct {
	Focused  bool
	Marker   string
	Label    string
	Metadata []string
	Children []treeNode
}

type treeOptions struct {
	Controls bool
	Width    int
}

const treeMetadataMinWidth = 48

func highlightLine(line string, width int) string {
	contentWidth := width
	if contentWidth > 0 {
		contentWidth = max(1, contentWidth-2)
		line = fitLine(line, contentWidth)
		line += strings.Repeat(" ", max(0, contentWidth-ansi.StringWidth(line)))
	}
	return styFocusBar.Render("│") + " " + line
}

func renderTree(nodes []treeNode, options treeOptions) []string {
	var lines []string
	var walk func([]treeNode, string)
	walk = func(level []treeNode, prefix string) {
		for index, node := range level {
			last := index == len(level)-1
			branch, childPrefix := "├─", prefix+"│  "
			if last {
				branch, childPrefix = "└─", prefix+"   "
			}
			content := styDim.Render(prefix+branch) + " "
			if options.Controls && node.Marker != "" {
				content += node.Marker + " "
			}
			content += node.Label
			line := content
			lineWidth := options.Width
			if options.Controls && lineWidth > 0 {
				lineWidth = max(1, lineWidth-2)
			}
			if metadata := treeMetadata(node.Metadata...); metadata != "" &&
				(lineWidth == 0 || lineWidth >= treeMetadataMinWidth) {
				if lineWidth > 0 {
					metadataWidth := ansi.StringWidth(metadata)
					contentBudget := max(1, lineWidth-metadataWidth-2)
					content = fitLine(content, contentBudget)
					gap := max(2, lineWidth-ansi.StringWidth(content)-metadataWidth)
					line = content + strings.Repeat(" ", gap) + metadata
				} else {
					line += "  " + metadata
				}
			}
			if options.Controls {
				if node.Focused {
					line = highlightLine(line, options.Width)
				} else {
					line = "  " + line
				}
			}
			lines = append(lines, line)
			walk(node.Children, childPrefix)
		}
	}
	walk(nodes, "")
	return lines
}

func treeMetadata(parts ...string) string {
	var visible []string
	for _, part := range parts {
		if part != "" {
			visible = append(visible, part)
		}
	}
	return strings.Join(visible, styDim.Render(" · "))
}

func treeCount(count int, singular string) string {
	label := singular
	if count != 1 {
		label += "s"
	}
	return styTreeMeta.Render(fmt.Sprintf("%d %s", count, label))
}

func treeNeutral(text string) string  { return styTreeMeta.Render(text) }
func treeLive(text string) string     { return styTreeLive.Render(text) }
func treeAdded(text string) string    { return styTreeAdd.Render(text) }
func treeExpanded(text string) string { return styWarn.Italic(true).Render(text) }
func treeMissing(text string) string  { return styTreeMiss.Render(text) }
func treeWarning(text string) string  { return styTreeWarn.Render("! " + text) }

type hintItem struct {
	Key   string
	Label string
}

func renderHints(items ...hintItem) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, styTitle.Bold(true).Render(item.Key)+" "+styDim.Render(item.Label))
	}
	return strings.Join(parts, styDim.Render("   ·   "))
}
