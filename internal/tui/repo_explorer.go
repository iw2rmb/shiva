package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func (model *rootModel) updateExplorerKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		model.activeRoute = RouteHome
		model.syncHomeSelection()
		return model, nil
	case "enter":
		if model.explorer.Selected < 0 || model.explorer.Selected >= len(model.explorer.Endpoints) {
			return model, nil
		}
		selected := model.explorer.Endpoints[model.explorer.Selected]
		model.setEndpointSelection(selected.Identity)
		model.activeRoute = RouteHome
		model.setHomeSelection(homeItemEndpoints)
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
	model.setRepoSelection(namespace, repo)
	model.setHomeSelection(homeItemEndpoints)
	model.activeRoute = RouteHome
	return model.ensureEndpointCatalogLoadCmd()
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
	repoLabel := model.selectedNamespace + "/" + model.selectedRepo
	leftPane := model.explorerListPane()
	rightPane := model.explorerDetailPane()
	body := strings.Join([]string{
		model.styles.Subtle("Repository: " + repoLabel),
		model.explorerTabRow(),
		"",
		renderExplorerPanes(model.styles, leftPane, rightPane, model.width),
	}, "\n")
	return model.layoutScreen(body, model.routeHelpView())
}

func (model *rootModel) explorerListPane() string {
	switch {
	case model.async.OperationList.Loading && len(model.explorer.Endpoints) == 0:
		return model.styles.EmptyBlock("Loading endpoints...")
	case model.async.OperationList.LastError != nil:
		return model.styles.ErrorBlock(
			"Failed to load endpoints.",
			model.async.OperationList.LastError.Error(),
		)
	case len(model.explorer.Endpoints) == 0:
		return model.styles.EmptyBlock("No endpoints found in repository.")
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
		labels = append(labels, model.styles.Tab(label, tab == model.explorer.Detail.ActiveTab))
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

func renderExplorerPanes(styles tuiStyles, left string, right string, width int) string {
	leftWidth, rightWidth, _, stacked := explorerPaneLayout(width, defaultListHeight)
	leftPane := styles.Pane("Endpoints", left, leftWidth)
	rightPane := styles.Pane("Details", right, rightWidth)
	if stacked {
		return strings.Join([]string{
			leftPane,
			"",
			rightPane,
		}, "\n")
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, leftPane, "  ", rightPane)
}
