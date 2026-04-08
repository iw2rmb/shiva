package tui

import (
	"fmt"
	"regexp"
	"strings"

	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"
	"github.com/iw2rmb/shiva/internal/tui/markdown"
)

var ansiEscapePattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func newDetailViewport(width int, height int) viewport.Model {
	model := viewport.New(
		viewport.WithWidth(width),
		viewport.WithHeight(height),
	)
	model.SoftWrap = true
	return model
}

func (model *rootModel) refreshExplorerDetailViewport() {
	markdownBody := model.explorerDetailMarkdown()
	width := model.explorer.Detail.Viewport.Width()
	rendered := model.markdown.Render(markdownBody, width)
	rendered = styleDetailSectionBadges(rendered)
	model.explorer.Detail.Viewport.SetContent(rendered)
}

var (
	detailSectionHeaderStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#000000")).
		Background(lipgloss.Color("#FFFFFF")).
		Padding(0, 1)
)

func styleDetailSectionBadges(rendered string) string {
	lines := strings.Split(rendered, "\n")
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		normalized := strings.Join(strings.Fields(stripANSIEscapeCodes(trimmed)), " ")
		var replacement string
		switch normalized {
		case "/: Path":
			replacement = renderDetailSectionHeader("/:", "PATH")
		case "?& Query":
			replacement = renderDetailSectionHeader("?&", "QUERY")
		case "{} Body":
			replacement = renderDetailSectionHeader("{}", "BODY")
		case "{} Body REQUIRED":
			replacement = renderDetailSectionHeaderWithChip("{}", "BODY", "REQUIRED")
		default:
			continue
		}
		prefixLen := len(line) - len(strings.TrimLeft(line, " \t"))
		lines[index] = line[:prefixLen] + replacement
	}
	return strings.Join(lines, "\n")
}

func stripANSIEscapeCodes(value string) string {
	return ansiEscapePattern.ReplaceAllString(value, "")
}

func renderDetailSectionHeader(badge string, label string) string {
	return detailSectionHeaderStyle.Render(badge + " " + label)
}

func renderDetailSectionHeaderWithChip(badge string, label string, chip string) string {
	return renderDetailSectionHeader(badge, label) + " " + responseErrorChipStyle.Render(chip)
}

func (model *rootModel) explorerDetailMarkdown() string {
	selected, ok := model.explorer.SelectedEndpoint()
	if !ok {
		return strings.Join([]string{
			"## Details",
			"",
			"Select an endpoint to show details.",
		}, "\n")
	}

	switch model.explorer.Detail.ActiveTab {
	case DetailTabRequest:
		if model.explorer.Detail.Operation == nil {
			if model.async.OperationDetail.LastError != nil {
				return renderDetailLoadError("Request", model.async.OperationDetail.LastError)
			}
			if model.async.OperationDetail.Loading {
				return strings.Join([]string{
					"Loading request detail...",
				}, "\n")
			}
			return strings.Join([]string{
				"No request detail available for the selected endpoint.",
			}, "\n")
		}
		return markdown.BuildRequest(markdown.EndpointInput{
			Method:    selected.Identity.Method,
			Path:      selected.Identity.Path,
			Operation: model.explorer.Detail.Operation.Body,
		})
	case DetailTabResponse:
		if model.explorer.Detail.Operation == nil {
			if model.async.OperationDetail.LastError != nil {
				return renderDetailLoadError("Response", model.async.OperationDetail.LastError)
			}
			if model.async.OperationDetail.Loading {
				return strings.Join([]string{
					"## Response",
					"",
					"Loading response detail...",
				}, "\n")
			}
			return markdown.BuildEmptySuccessResponses()
		}
		return markdown.BuildSuccessResponses(model.explorer.Detail.Operation.Body)
	case DetailTabErrors:
		if model.explorer.Detail.Operation == nil {
			if model.async.OperationDetail.LastError != nil {
				return renderDetailLoadError("Errors", model.async.OperationDetail.LastError)
			}
			if model.async.OperationDetail.Loading {
				return strings.Join([]string{
					"## Errors",
					"",
					"Loading endpoint detail...",
				}, "\n")
			}
			return markdown.BuildEmptyErrors()
		}
		return markdown.BuildErrors(model.explorer.Detail.Operation.Body)
	default:
		return "## Details"
	}
}

func renderDetailLoadError(title string, err error) string {
	return strings.Join([]string{
		fmt.Sprintf("## %s", title),
		"",
		fmt.Sprintf("Failed to load detail: `%s`", strings.TrimSpace(err.Error())),
	}, "\n")
}
