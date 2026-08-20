package tui

import "strings"

func (m *model) scrollViewport(rendered string, cursorLine, sticky int, scroll *int) string {
	lines := strings.Split(rendered, "\n")
	height := max(4, m.height-8)
	if len(lines) <= height {
		*scroll = 0
		return rendered
	}

	sticky = min(sticky, len(lines))
	body := lines[sticky:]
	bodyHeight := max(1, height-sticky)
	maxScroll := max(0, len(body)-bodyHeight)
	if cursorLine >= 0 {
		cursor := max(0, cursorLine-sticky)
		// Follow a focused row from the middle of the viewport. Clamping lets
		// the cursor move below center once the final screenful is visible.
		*scroll = max(0, min(cursor-bodyHeight/2, maxScroll))
	} else {
		*scroll = max(0, min(*scroll, maxScroll))
	}

	visible := append([]string(nil), lines[:sticky]...)
	visible = append(visible, body[*scroll:min(len(body), *scroll+bodyHeight)]...)
	return strings.Join(visible, "\n")
}

func (m *model) plannerViewport(rendered string, cursorLine int) string {
	return m.scrollViewport(rendered, cursorLine, 2, &m.plannerScroll)
}
