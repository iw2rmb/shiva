package tui

import (
	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
)

type routeHelpKeyMap struct {
	short []key.Binding
	full  [][]key.Binding
}

func (mapping routeHelpKeyMap) ShortHelp() []key.Binding {
	return mapping.short
}

func (mapping routeHelpKeyMap) FullHelp() [][]key.Binding {
	if len(mapping.full) == 0 {
		return [][]key.Binding{mapping.short}
	}
	return mapping.full
}

func newRouteHelpModel() help.Model {
	model := help.New()
	model.ShowAll = false
	return model
}

func keyHelp(keys string, description string) key.Binding {
	return key.NewBinding(
		key.WithKeys(keys),
		key.WithHelp(keys, description),
	)
}

func namespaceRouteHelp() routeHelpKeyMap {
	short := []key.Binding{
		keyHelp("enter", "open namespace"),
		keyHelp("esc", "back"),
		keyHelp("q", "quit"),
	}
	return routeHelpKeyMap{short: short}
}

func homeRouteHelp() routeHelpKeyMap {
	short := []key.Binding{
		keyHelp("enter", "focus pane"),
		keyHelp("backspace", "clear selection"),
		keyHelp("q", "quit"),
	}
	return routeHelpKeyMap{short: short}
}

func reposRouteHelp() routeHelpKeyMap {
	short := []key.Binding{
		keyHelp("enter", "open repo"),
		keyHelp("esc", "back"),
		keyHelp("q", "quit"),
	}
	return routeHelpKeyMap{short: short}
}

func explorerRouteHelp() routeHelpKeyMap {
	short := []key.Binding{
		keyHelp("enter", "select endpoint"),
		keyHelp("tab/shift+tab", "switch tab"),
		keyHelp("pgup/pgdown", "scroll details"),
		keyHelp("esc", "back"),
		keyHelp("q", "quit"),
	}
	return routeHelpKeyMap{short: short}
}

func (model *rootModel) routeHelp() routeHelpKeyMap {
	switch model.activeRoute {
	case RouteHome:
		return homeRouteHelp()
	case RouteNamespaces:
		return namespaceRouteHelp()
	case RouteRepos:
		return reposRouteHelp()
	case RouteRepoExplorer:
		return explorerRouteHelp()
	default:
		return routeHelpKeyMap{
			short: []key.Binding{keyHelp("q", "quit")},
		}
	}
}

func (model *rootModel) routeHelpView() string {
	return model.styles.Subtle(model.help.View(model.routeHelp()))
}
