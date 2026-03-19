package tui

import (
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
	case DetailTabEndpoints:
		if model.explorer.Detail.Operation == nil {
			if model.async.OperationDetail.Loading {
				return strings.Join([]string{
					"## Endpoint",
					"",
					"Loading endpoint detail...",
				}, "\n")
			}
			return strings.Join([]string{
				"## Endpoint",
				"",
				"No operation detail available for the selected endpoint.",
			}, "\n")
		}
		return markdown.BuildEndpoint(markdown.EndpointInput{
			Method:    selected.Identity.Method,
			Path:      selected.Identity.Path,
			Operation: model.explorer.Detail.Operation.Body,
		})
	case DetailTabServers:
		if model.explorer.Detail.Operation == nil {
			if model.async.OperationDetail.Loading {
				return strings.Join([]string{
					"## Servers",
					"",
					"Loading endpoint detail...",
				}, "\n")
			}
			return markdown.BuildEmptyServers()
		}
		if model.shouldLoadSelectedSpecDetail() && model.explorer.Detail.Spec == nil {
			if model.async.SpecDetail.Loading {
				return strings.Join([]string{
					"## Servers",
					"",
					"Loading spec-level servers...",
				}, "\n")
			}
			return markdown.BuildEmptyServers()
		}

		var specBody []byte
		if model.explorer.Detail.Spec != nil {
			specBody = model.explorer.Detail.Spec.Body
		}
		return markdown.BuildServers(model.explorer.Detail.Operation.Body, specBody)
	case DetailTabErrors:
		if model.explorer.Detail.Operation == nil {
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
