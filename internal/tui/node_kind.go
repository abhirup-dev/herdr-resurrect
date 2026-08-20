package tui

type nodeKind uint8

const (
	workspaceNode nodeKind = iota
	tabNode
	paneNode
)
