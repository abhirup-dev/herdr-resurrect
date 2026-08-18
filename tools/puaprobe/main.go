// puaprobe — ground truth for the kitty unicode-placeholder scheme inside a
// bubbletea v2 frame. Verifies: U+10EEEE survives, row diacritics (U+0305/
// U+030D) don't change the cell width, 256-color SGR passes through
// unconverted, and a repaint (second frame) re-emits the placeholder.
package main

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

type m struct{ frame int }

func (mo m) Init() tea.Cmd { return nil }
func (mo *m) Update(t tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := t.(time.Time); ok {
		if mo.frame == 0 {
			mo.frame = 1 // second frame: forces a repaint diff
			return mo, tick()
		}
		return mo, tea.Quit
	}
	return mo, nil
}

func tick() tea.Cmd { return func() tea.Msg { time.Sleep(300 * time.Millisecond); return time.Time{} } }

const (
	ph      = string(rune(0x10EEEE)) // kitty placeholder rune
	diacRow1 = string(rune(0x0305)) // macron = row/col 1
	diacRow2 = string(rune(0x030D)) // vertical line above = row/col 2
)

func (mo m) View() tea.View {
	var b strings.Builder
	if mo.frame == 1 {
		b.WriteString("  ")
	}
	b.WriteString("|\x1b[38;5;42m" + ph + diacRow1 + ph + "\x1b[39m|AB|")
	b.WriteString("\x1b[38;5;43m" + ph + diacRow2 + ph + "\x1b[39m|")
	b.WriteString("\n123456789.123456789.")
	return tea.NewView(b.String())
}

func main() {
	p := tea.NewProgram(&m{})
	go func() { time.Sleep(500 * time.Millisecond); p.Send(time.Time{}) }()
	p.Run()
}
