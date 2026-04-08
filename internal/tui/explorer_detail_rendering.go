package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/viewport"
	"github.com/iw2rmb/shiva/internal/tui/markdown"
)

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
	model.explorer.Detail.Viewport.SetContent(rendered)
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
					"## Request",
					"",
					"Loading request detail...",
				}, "\n")
			}
			return strings.Join([]string{
				"## Request",
				"",
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
