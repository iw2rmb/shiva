package tui

import (
	"os"
	"strings"

	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
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

	style := os.Getenv("GLAMOUR_STYLE")
	if style == "" {
		style = "light"
		if lipgloss.HasDarkBackground(os.Stdin, os.Stdout) {
			style = "dark"
		}
	}

	renderer, err := glamour.NewTermRenderer(
		glamour.WithStylePath(style),
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
