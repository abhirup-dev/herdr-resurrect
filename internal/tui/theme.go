package tui

import (
	"charm.land/bubbles/v2/list"
	"charm.land/lipgloss/v2"
)

// Use the terminal's ANSI palette rather than fixed RGB values. Herdr forwards
// the host palette into panes, so these semantic roles follow terminal theme
// changes even though Herdr does not yet expose its resolved UI palette to
// plugins.
var (
	colorAccent = lipgloss.Color("6")
	colorMuted  = lipgloss.Color("8")
	colorOK     = lipgloss.Color("2")
	colorWarn   = lipgloss.Color("3")
	colorBad    = lipgloss.Color("1")
	colorNice   = lipgloss.Color("5")

	styTitle = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	styDim   = lipgloss.NewStyle().Foreground(colorMuted)
	styOK    = lipgloss.NewStyle().Foreground(colorOK)
	styWarn  = lipgloss.NewStyle().Foreground(colorWarn)
	styBad   = lipgloss.NewStyle().Foreground(colorBad)
	styNice  = lipgloss.NewStyle().Foreground(colorNice)

	styTreeLabel = lipgloss.NewStyle().Bold(true)
	styTreeMeta  = styDim
	styTreeLive  = lipgloss.NewStyle().Foreground(colorOK).Bold(true)
	styTreeAdd   = lipgloss.NewStyle().Foreground(colorOK)
	styTreeMiss  = lipgloss.NewStyle().Foreground(colorWarn).Italic(true)
	styTreeWarn  = lipgloss.NewStyle().Foreground(colorBad).Bold(true)
	styFocusBar  = lipgloss.NewStyle().Foreground(colorAccent)

	styVerdict = map[string]lipgloss.Style{
		"KEEP-NATIVE": styOK,
		"REPLACE":     styBad,
		"RELAUNCH":    styWarn,
		"RESURRECT":   styNice,
		"SHELL":       styDim,
	}
)

func archiveItemStyles() list.DefaultItemStyles {
	normalTitle := lipgloss.NewStyle().Padding(0, 0, 0, 2)
	normalDesc := styDim.Padding(0, 0, 0, 2)
	selectedTitle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(colorAccent).
		Foreground(colorAccent).
		Bold(true).
		Padding(0, 0, 0, 1)
	selectedDesc := selectedTitle.Foreground(colorMuted).Bold(false)

	return list.DefaultItemStyles{
		NormalTitle:   normalTitle,
		NormalDesc:    normalDesc,
		SelectedTitle: selectedTitle,
		SelectedDesc:  selectedDesc,
		DimmedTitle:   normalTitle.Foreground(colorMuted),
		DimmedDesc:    normalDesc,
		FilterMatch:   lipgloss.NewStyle().Foreground(colorAccent).Underline(true),
	}
}
