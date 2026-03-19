package tui

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/iw2rmb/shiva/internal/cli/request"
)

func (model *rootModel) updateExplorerKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		model.activeRoute = RouteRepos
		model.syncRepoSelection()
		return model, nil
	default:
		var listCmd tea.Cmd
		model.explorer.List, listCmd = model.explorer.List.Update(msg)
		detailCmd := model.syncExplorerSelection()
		return model, batchCmds(listCmd, detailCmd)
	}
}

func (model *rootModel) openRepoExplorer(namespace string, repo string) tea.Cmd {
	model.activeRoute = RouteRepoExplorer
	model.explorer.Namespace = namespace
	model.explorer.Repo = repo
	model.explorer.Endpoints = nil
	model.explorer.Selected = -1
	model.explorer.Detail.ActiveTab = DetailTabEndpoints
	model.explorer.OperationCache = make(map[EndpointIdentity]OperationDetail)
	model.explorer.SpecCache = make(map[SpecIdentity]SpecDetail)
	model.clearExplorerDetailState()
	model.explorer.List.Title = "Endpoints"
	model.explorer.List.SetItems(nil)
	model.explorer.List.ResetSelected()

	token := model.beginOperationListLoad()
	return loadOperationListCmd(context.Background(), model.service, request.Envelope{
		Namespace: namespace,
		Repo:      repo,
	}, model.options, token)
}

func (model *rootModel) refreshExplorerList() {
	model.explorer.Endpoints = sortedEndpointEntries(model.explorer.Endpoints)
	model.explorer.List.Title = "Endpoints"
	if model.explorer.Namespace != "" && model.explorer.Repo != "" {
		model.explorer.List.Title = "Endpoints: " + model.explorer.Namespace + "/" + model.explorer.Repo
	}
	model.explorer.List.SetItems(endpointItems(model.explorer.Endpoints))
	if len(model.explorer.Endpoints) == 0 {
		model.explorer.Selected = -1
		model.explorer.List.ResetSelected()
		return
	}
	if model.explorer.Selected < 0 || model.explorer.Selected >= len(model.explorer.Endpoints) {
		model.explorer.Selected = 0
	}
	model.explorer.List.Select(model.explorer.Selected)
}

func (model *rootModel) syncExplorerSelection() tea.Cmd {
	index := model.explorer.List.Index()
	if len(model.explorer.Endpoints) == 0 || index < 0 || index >= len(model.explorer.Endpoints) {
		previous := model.explorer.Selected
		model.explorer.Selected = -1
		if previous >= 0 {
			model.clearExplorerDetailState()
		}
		return nil
	}
	previous := model.explorer.Selected
	model.explorer.Selected = index
	if previous != index {
		model.clearExplorerDetailState()
		return model.loadExplorerDetailForSelection()
	}
	return nil
}

func (model *rootModel) viewRepoExplorer() string {
	repoLabel := model.explorer.Namespace + "/" + model.explorer.Repo
	leftPane := model.explorerListPane()
	rightPane := model.explorerPlaceholderPane()
	return strings.Join([]string{
		"Shiva TUI",
		"",
		"Repository: " + repoLabel,
		model.explorerTabRow(),
		"",
		renderExplorerPanes(leftPane, rightPane, model.width),
		"",
		"up/down: select endpoint  esc: back  q: quit",
	}, "\n")
}

func (model *rootModel) explorerListPane() string {
	switch {
	case model.async.OperationList.Loading && len(model.explorer.Endpoints) == 0:
		return "Loading endpoints..."
	case model.async.OperationList.LastError != nil:
		return strings.Join([]string{
			"Failed to load endpoints.",
			model.async.OperationList.LastError.Error(),
		}, "\n")
	case len(model.explorer.Endpoints) == 0:
		return "No endpoints found in repository."
	default:
		return model.explorer.List.View()
	}
}

func (model *rootModel) explorerPlaceholderPane() string {
	selected, ok := model.explorer.SelectedEndpoint()
	if !ok {
		return strings.Join([]string{
			"Detail Placeholder",
			"",
			"Select an endpoint to show identity details.",
		}, "\n")
	}

	lines := []string{
		"Detail Placeholder",
		"",
		"tab: " + detailTabLabel(model.explorer.Detail.ActiveTab),
		"namespace: " + selected.Identity.Namespace,
		"repo: " + selected.Identity.Repo,
		"api: " + selected.Identity.API,
		"method: " + strings.ToUpper(selected.Identity.Method),
		"path: " + selected.Identity.Path,
	}
	if selected.Identity.OperationID != "" {
		lines = append(lines, "operation_id: "+selected.Identity.OperationID)
	}
	return strings.Join(lines, "\n")
}

func (model *rootModel) explorerTabRow() string {
	labels := make([]string, 0, 3)
	for _, tab := range []DetailTab{DetailTabEndpoints, DetailTabServers, DetailTabErrors} {
		label := detailTabLabel(tab)
		if tab == model.explorer.Detail.ActiveTab {
			label = "[" + label + "]"
		}
		labels = append(labels, label)
	}
	return strings.Join(labels, "  ")
}

func detailTabLabel(tab DetailTab) string {
	switch tab {
	case DetailTabEndpoints:
		return "Endpoints"
	case DetailTabServers:
		return "Servers"
	case DetailTabErrors:
		return "Errors"
	default:
		return string(tab)
	}
}

func renderExplorerPanes(left string, right string, width int) string {
	if width <= 0 {
		width = defaultListWidth
	}
	if width < 72 {
		return strings.Join([]string{
			"Endpoints",
			left,
			"",
			"Details",
			right,
		}, "\n")
	}

	gap := "  |  "
	leftWidth := (width - len(gap)) / 2
	rightWidth := width - len(gap) - leftWidth
	if leftWidth < 24 || rightWidth < 24 {
		return strings.Join([]string{
			"Endpoints",
			left,
			"",
			"Details",
			right,
		}, "\n")
	}

	leftLines := strings.Split(left, "\n")
	rightLines := strings.Split(right, "\n")
	maxLines := len(leftLines)
	if len(rightLines) > maxLines {
		maxLines = len(rightLines)
	}

	rendered := make([]string, 0, maxLines)
	for i := 0; i < maxLines; i++ {
		leftLine := ""
		if i < len(leftLines) {
			leftLine = leftLines[i]
		}
		rightLine := ""
		if i < len(rightLines) {
			rightLine = rightLines[i]
		}
		rendered = append(rendered, fitColumn(leftLine, leftWidth)+gap+trimColumn(rightLine, rightWidth))
	}
	return strings.Join(rendered, "\n")
}

func fitColumn(value string, width int) string {
	value = trimColumn(value, width)
	runeWidth := len([]rune(value))
	if runeWidth >= width {
		return value
	}
	return value + strings.Repeat(" ", width-runeWidth)
}

func trimColumn(value string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	return string(runes[:width])
}
