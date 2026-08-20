package tui

import (
	"strings"

	"charm.land/bubbles/v2/viewport"
)

func (m *model) scrollViewport(rendered string, cursorLine, sticky int, scroll *int) string {
	lines := strings.Split(rendered, "\n")
	height := max(4, m.height-8)
	sticky = min(sticky, len(lines))
	body := lines[sticky:]
	bodyHeight := max(1, height-sticky)
	bodyViewport := viewport.New(
		viewport.WithWidth(max(1, m.contentWidth())),
		viewport.WithHeight(bodyHeight),
	)
	bodyViewport.SetContentLines(body)
	bodyViewport.SetYOffset(*scroll)
	if cursorLine >= 0 {
		cursor := max(0, cursorLine-sticky)
		if cursor <= 1 {
			bodyViewport.GotoTop()
		} else {
			bodyViewport.EnsureVisible(cursor, 0, 0)
		}
	}
	*scroll = bodyViewport.YOffset()
	if sticky == 0 {
		return bodyViewport.View()
	}
	return strings.Join(lines[:sticky], "\n") + "\n" + bodyViewport.View()
}

func (m *model) plannerViewport(rendered string, cursorLine int) string {
	return m.scrollViewport(rendered, cursorLine, 2, &m.plannerScroll)
}
