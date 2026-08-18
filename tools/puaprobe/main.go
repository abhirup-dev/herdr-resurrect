// puaprobe — ground truth: does bubbletea v2 emit U+10EEEE (kitty unicode
// placeholder) to the tty, or does cellbuf drop it?
package main

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

type m struct{}

func (mo m) Init() tea.Cmd { return nil }
func (mo m) Update(t tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := t.(time.Time); ok {
		return mo, tea.Quit
	}
	return mo, nil
}
func (mo m) View() tea.View { return tea.NewView("A\U0010EEEEB") }

func main() {
	p := tea.NewProgram(&m{})
	go func() { time.Sleep(2 * time.Second); p.Send(time.Time{}) }()
	p.Run()
}
