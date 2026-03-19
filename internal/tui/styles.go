package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

type tuiStyles struct {
	header     lipgloss.Style
	subtle     lipgloss.Style
	tabActive  lipgloss.Style
	tabIdle    lipgloss.Style
	paneTitle  lipgloss.Style
	paneBody   lipgloss.Style
	errorBlock lipgloss.Style
	emptyBlock lipgloss.Style
}

func newTUIStyles() tuiStyles {
	return tuiStyles{
		header: lipgloss.NewStyle().Bold(true),
		subtle: lipgloss.NewStyle().Faint(true),
		tabActive: lipgloss.NewStyle().
			Bold(true).
			Underline(true),
		tabIdle:   lipgloss.NewStyle().Faint(true),
		paneTitle: lipgloss.NewStyle().Bold(true),
		paneBody: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			Padding(0, 1),
		errorBlock: lipgloss.NewStyle().
			BorderLeft(true).
			Bold(true).
			PaddingLeft(1),
		emptyBlock: lipgloss.NewStyle().
			BorderLeft(true).
			Faint(true).
			PaddingLeft(1),
	}
}

func (styles tuiStyles) Header(value string) string {
	return styles.header.Render(value)
}

func (styles tuiStyles) Tab(label string, active bool) string {
	if active {
		return styles.tabActive.Render(label)
	}
	return styles.tabIdle.Render(label)
}

func (styles tuiStyles) Pane(title string, body string, width int) string {
	titleView := styles.paneTitle.Render(title)
	bodyStyle := styles.paneBody
	if width > 0 {
		bodyStyle = bodyStyle.Width(width)
	}
	return titleView + "\n" + bodyStyle.Render(body)
}

func (styles tuiStyles) ErrorBlock(lines ...string) string {
	return styles.errorBlock.Render(strings.Join(lines, "\n"))
}

func (styles tuiStyles) EmptyBlock(lines ...string) string {
	return styles.emptyBlock.Render(strings.Join(lines, "\n"))
}

func (styles tuiStyles) Subtle(value string) string {
	return styles.subtle.Render(value)
}
