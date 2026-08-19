package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

var (
	styTreeLabel = lipgloss.NewStyle().Bold(true)
	styTreeMeta  = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Italic(true)
	styTreeLive  = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true).Italic(true)
	styTreeAdd   = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Italic(true)
	styTreeMiss  = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Italic(true)
	styTreeWarn  = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true).Italic(true)
	styTreeFocus = lipgloss.NewStyle().Reverse(true).Bold(true)
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
	plain := ansi.Strip(line)
	if width > 0 {
		plain = fitLine(plain, width)
		plain += strings.Repeat(" ", max(0, width-ansi.StringWidth(plain)))
	}
	return styTreeFocus.Render(plain)
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
			if metadata := treeMetadata(node.Metadata...); metadata != "" &&
				(options.Width == 0 || options.Width >= treeMetadataMinWidth) {
				if options.Width > 0 {
					metadataWidth := ansi.StringWidth(metadata)
					contentBudget := max(1, options.Width-metadataWidth-2)
					content = fitLine(content, contentBudget)
					gap := max(2, options.Width-ansi.StringWidth(content)-metadataWidth)
					line = content + strings.Repeat(" ", gap) + metadata
				} else {
					line += "  " + metadata
				}
			}
			if node.Focused {
				line = highlightLine(line, options.Width)
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
