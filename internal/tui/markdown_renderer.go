package tui

import (
	"os"
	"strings"

	"charm.land/glamour/v2"
)

type markdownRenderer interface {
	Render(markdown string, width int) string
}

const defaultGlamourStyle = "dark"

type glamourMarkdownRenderer struct {
	style     string
	renderers map[int]*glamour.TermRenderer
}

func newMarkdownRenderer() markdownRenderer {
	return &glamourMarkdownRenderer{
		style:     resolveGlamourStyle(),
		renderers: make(map[int]*glamour.TermRenderer),
	}
}

func resolveGlamourStyle() string {
	style := strings.TrimSpace(os.Getenv("GLAMOUR_STYLE"))
	if style != "" {
		return style
	}
	return defaultGlamourStyle
}

func (renderer *glamourMarkdownRenderer) Render(markdown string, width int) string {
	wrap := width
	if wrap < 20 {
		wrap = 20
	}

	termRenderer := renderer.renderers[wrap]
	if termRenderer == nil {
		created, err := glamour.NewTermRenderer(
			glamour.WithStylePath(renderer.style),
			glamour.WithWordWrap(wrap),
		)
		if err != nil {
			return markdown
		}
		renderer.renderers[wrap] = created
		termRenderer = created
	}

	rendered, err := termRenderer.Render(markdown)
	if err != nil {
		return markdown
	}
	return strings.TrimRight(rendered, "\n")
}
