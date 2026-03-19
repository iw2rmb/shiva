package tui

import (
	"strings"

	"github.com/charmbracelet/glamour"
)

type markdownRenderer interface {
	Render(markdown string, width int) string
}

type glamourMarkdownRenderer struct{}

func newMarkdownRenderer() markdownRenderer {
	return glamourMarkdownRenderer{}
}

func (glamourMarkdownRenderer) Render(markdown string, width int) string {
	wrap := width
	if wrap < 20 {
		wrap = 20
	}

	renderer, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(wrap),
	)
	if err != nil {
		return markdown
	}

	rendered, err := renderer.Render(markdown)
	if err != nil {
		return markdown
	}
	return strings.TrimRight(rendered, "\n")
}
