package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
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

func newRootModel(service BrowserService, route InitialRoute, options RequestOptions) rootModel {
	model := rootModel{
		service:      service,
		initialRoute: route,
		activeRoute:  route.Kind,
		options:      options,
		namespaces: NamespaceRouteState{
			Selected: -1,
		},
		repoList: RepoRouteState{
			Namespace: route.Namespace,
			Selected:  -1,
		},
		explorer: RepoExplorerRouteState{
			Namespace: route.Namespace,
			Repo:      route.Repo,
			Selected:  -1,
			Detail: DetailState{
				ActiveTab: DetailTabEndpoints,
			},
		},
	}
	return model
}

func (model rootModel) Init() tea.Cmd {
	return nil
}

func (model rootModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch typed := msg.(type) {
	case tea.KeyMsg:
		switch typed.String() {
		case "ctrl+c", "q":
			return model, tea.Quit
		}
	case tea.WindowSizeMsg:
		return model, func() tea.Msg {
			return resizeMsg{Width: typed.Width, Height: typed.Height}
		}
	case resizeMsg:
		model.width = typed.Width
		model.height = typed.Height
	case repoCatalogLoadedMsg:
		if !model.accepts(loadDomainRepoCatalog, typed.Token) {
			return model, nil
		}
		model.finishLoad(loadDomainRepoCatalog, typed.Token, nil)
		model.repos = append([]RepoEntry(nil), typed.Rows...)
	case operationListLoadedMsg:
		if !model.accepts(loadDomainOperationList, typed.Token) {
			return model, nil
		}
		model.finishLoad(loadDomainOperationList, typed.Token, nil)
		model.explorer.Endpoints = append([]EndpointEntry(nil), typed.Entries...)
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
	}

	return model, nil
}

func (model rootModel) View() string {
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
