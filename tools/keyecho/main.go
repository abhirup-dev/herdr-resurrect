// keyecho — prints tea KeyMsg.String() for every key pressed; ground truth
// for what the TUI's key dispatcher actually receives.
package main

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

type m struct{ keys []string }

func (mo *m) Init() tea.Cmd { return nil }
func (mo *m) Update(t tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := t.(tea.KeyPressMsg); ok {
		mo.keys = append(mo.keys, fmt.Sprintf("%q", k.String()))
		if len(mo.keys) > 8 {
			mo.keys = mo.keys[1:]
		}
		if k.String() == "ctrl+c" {
			return mo, tea.Quit
		}
	}
	return mo, nil
}
func (mo *m) View() tea.View {
	return tea.NewView("keys: " + strings.Join(mo.keys, " | "))
}

func main() {
	_, err := tea.NewProgram(&m{}).Run()
	if err != nil {
		panic(err)
	}
}
