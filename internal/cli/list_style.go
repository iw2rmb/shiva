package cli

import "charm.land/lipgloss/v2"

type listStyles struct {
	enabled       bool
	dimmed        lipgloss.Style
	muted         lipgloss.Style
	pathParam     lipgloss.Style
	defaultMethod lipgloss.Style
	getMethod     lipgloss.Style
	postMethod    lipgloss.Style
	putMethod     lipgloss.Style
	deleteMethod  lipgloss.Style
	operationID   lipgloss.Style
	summary       lipgloss.Style
}

func newListStyles(enabled bool) listStyles {
	return listStyles{
		enabled:       enabled,
		dimmed:        lipgloss.NewStyle().Faint(true),
		muted:         lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color("7")),
		pathParam:     lipgloss.NewStyle().Bold(true),
		defaultMethod: lipgloss.NewStyle().Bold(true),
		getMethod:     lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12")),
		postMethod:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10")),
		putMethod:     lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11")),
		deleteMethod:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("9")),
		operationID:   lipgloss.NewStyle().Bold(true),
		summary:       lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color("7")),
	}
}

func (s listStyles) renderDimmed(label string) string {
	if !s.enabled {
		return label
	}
	return s.dimmed.Render(label)
}

func (s listStyles) renderPathParam(value string) string {
	if !s.enabled {
		return value
	}
	return s.pathParam.Render(value)
}

func (s listStyles) renderMethod(method string) string {
	if !s.enabled {
		return method
	}
	switch method {
	case "GET":
		return s.getMethod.Render(method)
	case "POST":
		return s.postMethod.Render(method)
	case "PUT":
		return s.putMethod.Render(method)
	case "DELETE":
		return s.deleteMethod.Render(method)
	default:
		return s.defaultMethod.Render(method)
	}
}

func (s listStyles) renderOperationID(value string) string {
	if !s.enabled {
		return value
	}
	return s.operationID.Render(value)
}

func (s listStyles) renderSummary(value string) string {
	if !s.enabled {
		return value
	}
	return s.summary.Render(value)
}

func (s listStyles) renderMuted(value string) string {
	if !s.enabled {
		return value
	}
	return s.muted.Render(value)
}

func (s listStyles) renderRepoName(value string, dimmed bool) string {
	if !dimmed {
		return value
	}
	return s.renderDimmed(value)
}

func renderedWidth(value string) int {
	return lipgloss.Width(value)
}
