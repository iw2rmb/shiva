package cli

import "charm.land/lipgloss/v2"

type listStyles struct {
	enabled       bool
	zeroOps       lipgloss.Style
	pathParam     lipgloss.Style
	defaultMethod lipgloss.Style
	getMethod     lipgloss.Style
	postMethod    lipgloss.Style
	putMethod     lipgloss.Style
	deleteMethod  lipgloss.Style
}

func newListStyles(enabled bool) listStyles {
	return listStyles{
		enabled:       enabled,
		zeroOps:       lipgloss.NewStyle().Faint(true),
		pathParam:     lipgloss.NewStyle().Bold(true),
		defaultMethod: lipgloss.NewStyle().Bold(true),
		getMethod:     lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12")),
		postMethod:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10")),
		putMethod:     lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11")),
		deleteMethod:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("9")),
	}
}

func (s listStyles) renderZeroOps(label string) string {
	if !s.enabled {
		return label
	}
	return s.zeroOps.Render(label)
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

func renderedWidth(value string) int {
	return lipgloss.Width(value)
}
