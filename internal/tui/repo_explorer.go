package tui

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/iw2rmb/shiva/internal/cli/request"
)

func (model *rootModel) updateExplorerKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		model.activeRoute = RouteRepos
		model.syncRepoSelection()
		return model, nil
	case "tab":
		return model, model.switchExplorerTab(1)
	case "shift+tab":
		return model, model.switchExplorerTab(-1)
	default:
		var listCmd tea.Cmd
		model.explorer.List, listCmd = model.explorer.List.Update(msg)
		var viewportCmd tea.Cmd
		if shouldRouteKeyToDetailViewport(msg) {
			model.explorer.Detail.Viewport, viewportCmd = model.explorer.Detail.Viewport.Update(msg)
		}
		detailCmd := model.syncExplorerSelection()
		return model, batchCmds(listCmd, viewportCmd, detailCmd)
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
	model.refreshExplorerDetailViewport()
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
			model.refreshExplorerDetailViewport()
		}
		return nil
	}
	previous := model.explorer.Selected
	model.explorer.Selected = index
	if previous != index {
		model.clearExplorerDetailState()
		model.refreshExplorerDetailViewport()
		return model.loadExplorerDetailForSelection()
	}
	return nil
}

func (model *rootModel) viewRepoExplorer() string {
	repoLabel := model.explorer.Namespace + "/" + model.explorer.Repo
	leftPane := model.explorerListPane()
	rightPane := model.explorerDetailPane()
	return strings.Join([]string{
		"Shiva TUI",
		"",
		"Repository: " + repoLabel,
		model.explorerTabRow(),
		"",
		renderExplorerPanes(leftPane, rightPane, model.width),
		"",
		"up/down: select endpoint  tab/shift+tab: switch tab  pgup/pgdown: scroll details  esc: back  q: quit",
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

func (model *rootModel) explorerDetailPane() string {
	return model.explorer.Detail.Viewport.View()
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

func (model *rootModel) switchExplorerTab(delta int) tea.Cmd {
	tabs := []DetailTab{DetailTabEndpoints, DetailTabServers, DetailTabErrors}
	index := 0
	for i, tab := range tabs {
		if tab == model.explorer.Detail.ActiveTab {
			index = i
			break
		}
	}

	index = (index + delta + len(tabs)) % len(tabs)
	model.explorer.Detail.ActiveTab = tabs[index]
	model.refreshExplorerDetailViewport()
	return model.loadSelectedSpecDetailIfNeeded()
}

func shouldRouteKeyToDetailViewport(msg tea.KeyPressMsg) bool {
	switch msg.Code {
	case tea.KeyPgUp, tea.KeyPgDown, tea.KeyHome, tea.KeyEnd:
		return true
	}
	switch msg.String() {
	case "ctrl+u", "ctrl+d":
		return true
	default:
		return false
	}
}

func renderExplorerPanes(left string, right string, width int) string {
	leftWidth, rightWidth, _, stacked := explorerPaneLayout(width, defaultListHeight)
	if stacked {
		return strings.Join([]string{
			"Endpoints",
			left,
			"",
			"Details",
			right,
		}, "\n")
	}

	leftPane := lipgloss.NewStyle().Width(leftWidth).Render(left)
	rightPane := lipgloss.NewStyle().Width(rightWidth).Render(right)
	return lipgloss.JoinHorizontal(lipgloss.Top, leftPane, "  |  ", rightPane)
}
