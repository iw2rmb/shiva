package tui

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

type rootModel struct {
	service      BrowserService
	initialRoute InitialRoute
	activeRoute  RouteKind
	options      RequestOptions
	repos        []RepoEntry
	namespaces   NamespaceRouteState
	repoList     RepoRouteState
	explorer     RepoExplorerRouteState
	async        AsyncState
	width        int
	height       int
}

func newRootModel(service BrowserService, route InitialRoute, options RequestOptions) *rootModel {
	model := &rootModel{
		service:      service,
		initialRoute: route,
		activeRoute:  route.Kind,
		options:      options,
		namespaces: NamespaceRouteState{
			Selected: -1,
			List:     newNamespaceList(),
		},
		repoList: RepoRouteState{
			Namespace: route.Namespace,
			Selected:  -1,
			List:      newRepoList(),
		},
		explorer: RepoExplorerRouteState{
			Namespace: route.Namespace,
			Repo:      route.Repo,
			Selected:  -1,
			List:      newEndpointList(),
			Detail: DetailState{
				ActiveTab: DetailTabEndpoints,
			},
		},
	}
	model.resizeLists()
	return model
}

func (model *rootModel) Init() tea.Cmd {
	token := model.beginRepoCatalogLoad()
	return loadRepoCatalogCmd(context.Background(), model.service, model.options, token)
}

func (model *rootModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch typed := msg.(type) {
	case tea.KeyPressMsg:
		if model.shouldQuit(typed.String()) {
			return model, tea.Quit
		}
		return model.updateKey(typed)
	case tea.WindowSizeMsg:
		return model, func() tea.Msg {
			return resizeMsg{Width: typed.Width, Height: typed.Height}
		}
	case resizeMsg:
		model.width = typed.Width
		model.height = typed.Height
		model.resizeLists()
	case repoCatalogLoadedMsg:
		if !model.accepts(loadDomainRepoCatalog, typed.Token) {
			return model, nil
		}
		model.finishLoad(loadDomainRepoCatalog, typed.Token, nil)
		model.repos = append([]RepoEntry(nil), typed.Rows...)
		model.refreshCatalogViews()
		return model, model.initialRouteCmd()
	case operationListLoadedMsg:
		if !model.accepts(loadDomainOperationList, typed.Token) {
			return model, nil
		}
		model.finishLoad(loadDomainOperationList, typed.Token, nil)
		model.explorer.Endpoints = sortedEndpointEntries(typed.Entries)
		model.refreshExplorerList()
	case operationDetailLoadedMsg:
		if !model.accepts(loadDomainOperationDetail, typed.Token) {
			return model, nil
		}
		model.finishLoad(loadDomainOperationDetail, typed.Token, nil)
		detail := typed.Detail
		model.explorer.Detail.Operation = &detail
	case specDetailLoadedMsg:
		if !model.accepts(loadDomainSpecDetail, typed.Token) {
			return model, nil
		}
		model.finishLoad(loadDomainSpecDetail, typed.Token, nil)
		detail := typed.Detail
		model.explorer.Detail.Spec = &detail
	case loadFailedMsg:
		if !model.accepts(typed.Domain, typed.Token) {
			return model, nil
		}
		model.finishLoad(typed.Domain, typed.Token, typed.Err)
		if typed.Domain == loadDomainRepoCatalog {
			model.refreshCatalogViews()
		}
	}

	return model, nil
}

func (model *rootModel) shouldQuit(key string) bool {
	switch key {
	case "ctrl+c", "q":
		return true
	default:
		return false
	}
}

func (model *rootModel) updateKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch model.activeRoute {
	case RouteNamespaces:
		return model.updateNamespacesKey(msg)
	case RouteRepos:
		return model.updateReposKey(msg)
	case RouteRepoExplorer:
		return model.updateExplorerKey(msg)
	default:
		return model, nil
	}
}

func (model *rootModel) updateNamespacesKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		if !model.canEnterRepoList() {
			return model, nil
		}
		selection := model.namespaces.Entries[model.namespaces.Selected]
		model.repoList.Namespace = selection.Namespace
		model.activeRoute = RouteRepos
		model.refreshRepoList()
		return model, nil
	default:
		var cmd tea.Cmd
		model.namespaces.List, cmd = model.namespaces.List.Update(msg)
		model.syncNamespaceSelection()
		return model, cmd
	}
}

func (model *rootModel) updateReposKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		model.activeRoute = RouteNamespaces
		model.syncNamespaceSelection()
		return model, nil
	case "enter":
		if model.repoList.Selected < 0 || model.repoList.Selected >= len(model.repoList.Entries) {
			return model, nil
		}
		selected := model.repoList.Entries[model.repoList.Selected]
		return model, model.openRepoExplorer(selected.Namespace, selected.Repo)
	default:
		var cmd tea.Cmd
		model.repoList.List, cmd = model.repoList.List.Update(msg)
		model.syncRepoSelection()
		return model, cmd
	}
}

func (model *rootModel) canEnterRepoList() bool {
	return model.namespaces.Selected >= 0 && model.namespaces.Selected < len(model.namespaces.Entries)
}

func (model *rootModel) refreshCatalogViews() {
	model.refreshNamespaceList()
	model.refreshRepoList()
}

func (model *rootModel) refreshNamespaceList() {
	model.namespaces.Entries = namespaceEntriesFromRepos(model.repos)
	model.namespaces.List.SetItems(namespaceItems(model.namespaces.Entries))
	if len(model.namespaces.Entries) == 0 {
		model.namespaces.Selected = -1
		model.namespaces.List.ResetSelected()
		return
	}
	if model.namespaces.Selected < 0 || model.namespaces.Selected >= len(model.namespaces.Entries) {
		model.namespaces.Selected = 0
	}
	model.namespaces.List.Select(model.namespaces.Selected)
}

func (model *rootModel) refreshRepoList() {
	model.repoList.Entries = repoEntriesByNamespace(model.repos, model.repoList.Namespace)
	model.repoList.List.Title = "Repositories"
	if model.repoList.Namespace != "" {
		model.repoList.List.Title = "Repositories: " + model.repoList.Namespace
	}
	model.repoList.List.SetItems(repoItems(model.repoList.Entries))
	if len(model.repoList.Entries) == 0 {
		model.repoList.Selected = -1
		model.repoList.List.ResetSelected()
		return
	}
	if model.repoList.Selected < 0 || model.repoList.Selected >= len(model.repoList.Entries) {
		model.repoList.Selected = 0
	}
	model.repoList.List.Select(model.repoList.Selected)
}

func (model *rootModel) syncNamespaceSelection() {
	index := model.namespaces.List.Index()
	if len(model.namespaces.Entries) == 0 || index < 0 || index >= len(model.namespaces.Entries) {
		model.namespaces.Selected = -1
		return
	}
	model.namespaces.Selected = index
}

func (model *rootModel) syncRepoSelection() {
	index := model.repoList.List.Index()
	if len(model.repoList.Entries) == 0 || index < 0 || index >= len(model.repoList.Entries) {
		model.repoList.Selected = -1
		return
	}
	model.repoList.Selected = index
}

func (model *rootModel) resizeLists() {
	width, height := listSize(model.width, model.height)
	model.namespaces.List.SetSize(width, height)
	model.repoList.List.SetSize(width, height)
	explorerWidth, explorerHeight := endpointListSize(model.width, model.height)
	model.explorer.List.SetSize(explorerWidth, explorerHeight)
}

func (model *rootModel) initialRouteCmd() tea.Cmd {
	switch model.initialRoute.Kind {
	case RouteRepos:
		model.activeRoute = RouteRepos
		model.repoList.Namespace = model.initialRoute.Namespace
		model.refreshRepoList()
	case RouteRepoExplorer:
		model.repoList.Namespace = model.initialRoute.Namespace
		model.refreshRepoList()
		return model.openRepoExplorer(model.initialRoute.Namespace, model.initialRoute.Repo)
	}
	return nil
}

func (model *rootModel) View() tea.View {
	var s string
	switch model.activeRoute {
	case RouteNamespaces:
		s = model.viewNamespaces()
	case RouteRepos:
		s = model.viewRepos()
	case RouteRepoExplorer:
		s = model.viewRepoExplorer()
	default:
		s = model.viewPlaceholder()
	}
	v := tea.NewView(s)
	v.AltScreen = true
	return v
}

func (model *rootModel) viewNamespaces() string {
	if model.async.RepoCatalog.Loading && len(model.repos) == 0 {
		return "Shiva TUI\n\nLoading repositories..."
	}
	if model.async.RepoCatalog.LastError != nil {
		return strings.Join([]string{
			"Shiva TUI",
			"",
			"Namespaces",
			"",
			"Failed to load repositories.",
			model.async.RepoCatalog.LastError.Error(),
		}, "\n")
	}
	if len(model.namespaces.Entries) == 0 {
		return "Shiva TUI\n\nNamespaces\n\nNo namespaces found."
	}
	return strings.Join([]string{
		"Shiva TUI",
		"",
		model.namespaces.List.View(),
		"",
		"enter: open namespace  q: quit",
	}, "\n")
}

func (model *rootModel) viewRepos() string {
	if model.async.RepoCatalog.Loading && len(model.repos) == 0 {
		return "Shiva TUI\n\nLoading repositories..."
	}
	if model.async.RepoCatalog.LastError != nil {
		return strings.Join([]string{
			"Shiva TUI",
			"",
			"Repositories",
			"",
			"Failed to load repositories.",
			model.async.RepoCatalog.LastError.Error(),
		}, "\n")
	}
	if len(model.repoList.Entries) == 0 {
		return strings.Join([]string{
			"Shiva TUI",
			"",
			"Repositories: " + model.repoList.Namespace,
			"",
			"No repositories found in namespace.",
			"",
			"esc: back  q: quit",
		}, "\n")
	}
	return strings.Join([]string{
		"Shiva TUI",
		"",
		model.repoList.List.View(),
		"",
		"enter: open repo  esc: back  q: quit",
	}, "\n")
}

func (model *rootModel) viewPlaceholder() string {
	lines := []string{
		"Shiva TUI",
		"",
		routeLabel(model.activeRoute, model.repoList.Namespace, model.explorer.Namespace, model.explorer.Repo),
	}
	if model.options.Profile != "" {
		lines = append(lines, "profile: "+model.options.Profile)
	}
	if model.options.Offline {
		lines = append(lines, "offline: true")
	}
	if model.width > 0 || model.height > 0 {
		lines = append(lines, fmt.Sprintf("size: %dx%d", model.width, model.height))
	}
	lines = append(lines, "", "Press q to quit.")
	return strings.Join(lines, "\n")
}

func routeLabel(route RouteKind, repoNamespace string, explorerNamespace string, repo string) string {
	switch route {
	case RouteNamespaces:
		return "start: namespaces"
	case RouteRepos:
		return "start: namespace " + repoNamespace
	case RouteRepoExplorer:
		return "start: repo " + explorerNamespace + "/" + repo
	default:
		return "start: unknown"
	}
}

func (model *rootModel) beginRepoCatalogLoad() RequestToken {
	return model.beginLoad(loadDomainRepoCatalog)
}

func (model *rootModel) beginOperationListLoad() RequestToken {
	return model.beginLoad(loadDomainOperationList)
}

func (model *rootModel) beginOperationDetailLoad() RequestToken {
	return model.beginLoad(loadDomainOperationDetail)
}

func (model *rootModel) beginSpecDetailLoad() RequestToken {
	return model.beginLoad(loadDomainSpecDetail)
}

func (model *rootModel) beginLoad(domain loadDomain) RequestToken {
	model.async.nextToken++
	token := model.async.nextToken
	state := model.loadState(domain)
	state.ActiveToken = token
	state.Loading = true
	state.LastError = nil
	return token
}

func (model *rootModel) accepts(domain loadDomain, token RequestToken) bool {
	return model.loadState(domain).ActiveToken == token
}

func (model *rootModel) finishLoad(domain loadDomain, token RequestToken, err error) {
	state := model.loadState(domain)
	if state.ActiveToken != token {
		return
	}
	state.Loading = false
	state.LastError = err
}

func (model *rootModel) loadState(domain loadDomain) *asyncLoadState {
	switch domain {
	case loadDomainRepoCatalog:
		return &model.async.RepoCatalog
	case loadDomainOperationList:
		return &model.async.OperationList
	case loadDomainOperationDetail:
		return &model.async.OperationDetail
	case loadDomainSpecDetail:
		return &model.async.SpecDetail
	default:
		panic(fmt.Sprintf("unsupported load domain %q", domain))
	}
}
