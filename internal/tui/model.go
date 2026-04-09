package tui

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/paginator"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/iw2rmb/shiva/internal/cli/request"
)

const (
	homeItemNamespaces = 0
	homeItemRepos      = 1
	homeItemAPIs       = 2
	homeItemEndpoints  = 3
)

type rootModel struct {
	service      BrowserService
	initialRoute InitialRoute
	activeRoute  RouteKind
	options      RequestOptions
	markdown     markdownRenderer
	help         help.Model
	styles       tuiStyles
	repos        []RepoEntry
	home         HomeRouteState
	namespaces   NamespaceRouteState
	repoList     RepoRouteState
	apiList      APIRouteState
	explorer     RepoExplorerRouteState
	async        AsyncState
	width        int
	height       int

	selectedNamespace string
	selectedRepo      string
	selectedAPI       string
	selectedEndpoint  *EndpointIdentity

	apiCatalogByScope       map[string][]APIEntry
	endpointCatalogByRepo   map[string][]EndpointEntry
	endpointHasMoreByScope  map[string]bool
	namespaceCatalogHasMore bool
	repoCatalogHasMore      map[string]bool

	namespaceCatalogCount CatalogCount
	repoCatalogCount      map[string]CatalogCount
	apiCatalogCount       map[string]CatalogCount
	operationCatalogCount map[string]CatalogCount
}

func newRootModel(service BrowserService, route InitialRoute, options RequestOptions) *rootModel {
	initialFocusRoute := route.Kind
	switch route.Kind {
	case RouteHome, RouteRepos, RouteRepoExplorer:
		initialFocusRoute = RouteHome
	}

	model := &rootModel{
		service:      service,
		initialRoute: route,
		activeRoute:  initialFocusRoute,
		options:      options,
		markdown:     newMarkdownRenderer(),
		help:         newRouteHelpModel(),
		styles:       newTUIStyles(),
		home: HomeRouteState{
			Entries:  defaultHomeEntries(),
			Selected: -1,
			List:     newShivaList(),
		},
		namespaces: NamespaceRouteState{
			Selected: -1,
			List:     newNamespaceList(),
			Pager:    newPaginator(),
		},
		repoList: RepoRouteState{
			Selected: -1,
			List:     newRepoList(),
			Pager:    newPaginator(),
		},
		apiList: APIRouteState{
			Selected: -1,
			List:     newAPIList(),
			Pager:    newPaginator(),
			Detail: APIDetailState{
				ActiveTab: APIDetailTabData,
				Viewport:  newDetailViewport(defaultListWidth, defaultViewportHeight),
			},
			SpecCache:  make(map[SpecIdentity]SpecDetail),
			IssueCache: make(map[SpecIdentity]APIIssuesDetail),
		},
		explorer: RepoExplorerRouteState{
			Selected: -1,
			List:     newEndpointList(),
			Pager:    newPaginator(),
			Detail: DetailState{
				ActiveTab: DetailTabRequest,
				Viewport:  newDetailViewport(defaultListWidth, defaultViewportHeight),
			},
			OperationCache: make(map[EndpointIdentity]OperationDetail),
			SpecCache:      make(map[SpecIdentity]SpecDetail),
		},
		apiCatalogByScope:       make(map[string][]APIEntry),
		endpointCatalogByRepo:   make(map[string][]EndpointEntry),
		endpointHasMoreByScope:  make(map[string]bool),
		namespaceCatalogHasMore: true,
		repoCatalogHasMore:      make(map[string]bool),
		repoCatalogCount:        make(map[string]CatalogCount),
		apiCatalogCount:         make(map[string]CatalogCount),
		operationCatalogCount:   make(map[string]CatalogCount),
	}

	model.seedSelectionFromInitialRoute(route)
	model.refreshHomeEntryPresentation()
	model.refreshHomeList()
	model.refreshRepoList()
	model.refreshAPIList()
	model.refreshExplorerList()
	model.resizeLists()
	model.refreshAPIDetailViewport()
	model.refreshExplorerDetailViewport()
	return model
}

func (model *rootModel) seedSelectionFromInitialRoute(route InitialRoute) {
	switch route.Kind {
	case RouteRepos:
		model.selectedNamespace = route.Namespace
		model.home.Selected = homeItemRepos
		model.explorer.Namespace = route.Namespace
	case RouteAPIs:
		model.selectedNamespace = route.Namespace
		model.selectedRepo = route.Repo
		model.home.Selected = homeItemAPIs
		model.apiList.Namespace = route.Namespace
		model.apiList.Repo = route.Repo
	case RouteRepoExplorer:
		model.selectedNamespace = route.Namespace
		model.selectedRepo = route.Repo
		model.home.Selected = homeItemEndpoints
		model.explorer.Namespace = route.Namespace
		model.explorer.Repo = route.Repo
	default:
		model.home.Selected = homeItemNamespaces
	}
}

func (model *rootModel) Init() tea.Cmd {
	cmds := []tea.Cmd{}

	countToken := model.beginNamespaceCountLoad()
	cmds = append(cmds, loadNamespaceCountCmd(context.Background(), model.service, model.options, countToken))

	switch model.home.Selected {
	case homeItemNamespaces:
		if cmd := model.ensureNamespaceCatalogLoadCmd(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case homeItemRepos:
		if cmd := model.ensureNamespaceCatalogLoadCmd(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if cmd := model.ensureRepoCatalogLoadCmd(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if cmd := model.ensureRepoCountLoadCmd(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case homeItemAPIs:
		if model.initialRoute.Kind != RouteAPIs && model.initialRoute.Kind != RouteRepoExplorer {
			if cmd := model.ensureRepoCatalogLoadCmd(); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		if cmd := model.ensureAPICountLoadCmd(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if cmd := model.ensureAPICatalogLoadCmd(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case homeItemEndpoints:
		if model.initialRoute.Kind != RouteRepoExplorer {
			if cmd := model.ensureRepoCatalogLoadCmd(); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		if cmd := model.ensureAPICatalogLoadCmd(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if cmd := model.ensureAPICountLoadCmd(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if cmd := model.ensureEndpointCatalogLoadCmd(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	cmds = append(cmds, requestWindowSizeCmd())
	return tea.Batch(cmds...)
}

func requestWindowSizeCmd() tea.Cmd {
	return func() tea.Msg {
		return tea.RequestWindowSize()
	}
}

func (model *rootModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch typed := msg.(type) {
	case tea.KeyPressMsg:
		if model.shouldQuit(typed.String()) {
			return model, tea.Quit
		}
		return model.updateKey(typed)
	case list.FilterMatchesMsg:
		return model.updateActiveRouteList(typed)
	case tea.WindowSizeMsg:
		return model, func() tea.Msg {
			return resizeMsg{Width: typed.Width, Height: typed.Height}
		}
	case resizeMsg:
		previousNamespacePerPage := model.namespaceItemsPerPage()
		previousRepoPerPage := model.repoItemsPerPage()
		previousSpecPerPage := model.apiItemsPerPage()
		previousEndpointPerPage := model.endpointItemsPerPage()
		model.width = typed.Width
		model.height = typed.Height
		model.resizeLists()
		model.syncKnownPaginators()
		model.refreshAPIDetailViewportIfVisible()
		model.refreshExplorerDetailViewportIfVisible()
		if model.activePageSizeChanged(
			previousNamespacePerPage,
			previousRepoPerPage,
			previousSpecPerPage,
			previousEndpointPerPage,
		) {
			return model, model.reloadActiveCatalogForResize()
		}
	case repoCatalogLoadedMsg:
		if !model.accepts(loadDomainRepoCatalog, typed.Token) {
			return model, nil
		}
		model.finishLoad(loadDomainRepoCatalog, typed.Token, nil)
		model.repos = append([]RepoEntry(nil), typed.Rows...)
		limit := typed.Limit
		if limit < 1 {
			limit = int32(model.repoItemsPerPage())
		}
		model.repoCatalogHasMore[model.selectedNamespace] = int32(len(typed.Rows)) >= limit
		model.refreshRepoList()
		model.refreshAPIList()
		model.refreshExplorerList()
		model.refreshHomeEntryPresentation()
		model.refreshHomeList()
		if model.home.Selected == homeItemEndpoints {
			return model, batchCmds(model.ensureAPICatalogLoadCmd(), model.ensureEndpointCatalogLoadCmd())
		}
		return model, nil
	case apiCatalogLoadedMsg:
		if !model.accepts(loadDomainAPICatalog, typed.Token) {
			return model, nil
		}
		model.finishLoad(loadDomainAPICatalog, typed.Token, nil)
		key := apiScopeKey(typed.Namespace, typed.Repo)
		model.apiCatalogByScope[key] = append([]APIEntry(nil), typed.Rows...)
		model.refreshAPIList()
		model.refreshAPIDetailViewportIfVisible()
		model.refreshHomeEntryPresentation()
		model.refreshHomeList()
		if model.home.Selected == homeItemEndpoints {
			return model, model.ensureEndpointCatalogLoadCmd()
		}
		if model.home.Selected == homeItemAPIs {
			return model, model.ensureAPIDetailLoadForTab()
		}
		return model, nil
	case namespaceCatalogLoadedMsg:
		if !model.accepts(loadDomainNamespaces, typed.Token) {
			return model, nil
		}
		model.finishLoad(loadDomainNamespaces, typed.Token, nil)
		model.namespaces.Entries = append([]NamespaceEntry(nil), typed.Rows...)
		limit := typed.Limit
		if limit < 1 {
			limit = int32(model.namespaceItemsPerPage())
		}
		model.namespaceCatalogHasMore = int32(len(typed.Rows)) >= limit
		model.refreshNamespaceList()
		return model, nil
	case namespaceCountLoadedMsg:
		if !model.accepts(loadDomainNamespaceCount, typed.Token) {
			return model, nil
		}
		model.finishLoad(loadDomainNamespaceCount, typed.Token, nil)
		model.namespaceCatalogCount = typed.Count
		model.syncPaginator(&model.namespaces.Pager, typed.Count.TotalCount, model.namespaceItemsPerPage())
		model.home.Entries = withHomeNamespaceCount(model.home.Entries, typed.Count.TotalCount)
		model.refreshHomeEntryPresentation()
		model.refreshHomeList()
		model.resizeLists()
		return model, nil
	case repoCountLoadedMsg:
		if !model.accepts(loadDomainRepoCount, typed.Token) {
			return model, nil
		}
		model.finishLoad(loadDomainRepoCount, typed.Token, nil)
		model.repoCatalogCount[typed.Namespace] = typed.Count
		model.syncPaginator(&model.repoList.Pager, typed.Count.TotalCount, model.repoItemsPerPage())
		model.resizeLists()
		return model, nil
	case apiCountLoadedMsg:
		if !model.accepts(loadDomainAPICount, typed.Token) {
			return model, nil
		}
		model.finishLoad(loadDomainAPICount, typed.Token, nil)
		model.apiCatalogCount[apiScopeKey(typed.Namespace, typed.Repo)] = typed.Count
		model.syncPaginator(&model.apiList.Pager, typed.Count.TotalCount, model.apiItemsPerPage())
		model.resizeLists()
		return model, nil
	case operationCountLoadedMsg:
		if !model.accepts(loadDomainOperationCount, typed.Token) {
			return model, nil
		}
		model.finishLoad(loadDomainOperationCount, typed.Token, nil)
		model.operationCatalogCount[repoPath(typed.Namespace, typed.Repo)] = typed.Count
		model.syncPaginator(&model.explorer.Pager, typed.Count.TotalCount, model.endpointItemsPerPage())
		model.resizeLists()
		return model, nil
	case operationListLoadedMsg:
		if !model.accepts(loadDomainOperationList, typed.Token) {
			return model, nil
		}
		model.finishLoad(loadDomainOperationList, typed.Token, nil)
		key := model.endpointScopeKey()
		model.endpointCatalogByRepo[key] = append([]EndpointEntry(nil), typed.Entries...)
		limit := typed.Limit
		if limit < 1 {
			limit = int32(model.endpointItemsPerPage())
		}
		model.endpointHasMoreByScope[key] = int32(len(typed.Entries)) >= limit
		maxLength := int64(0)
		for _, entry := range typed.Entries {
			label := strings.ToUpper(strings.TrimSpace(entry.Identity.Method)) + " " + strings.TrimSpace(entry.Identity.Path)
			length := int64(len([]rune(strings.TrimSpace(label))))
			if length > maxLength {
				maxLength = length
			}
		}
		model.operationCatalogCount[key] = CatalogCount{
			TotalCount:    int64(len(typed.Entries)),
			MaxItemLength: maxLength,
		}
		model.refreshExplorerList()
		model.refreshExplorerDetailViewport()
		detailCmd := model.loadExplorerDetailForSelection()
		countCmd := tea.Cmd(nil)
		if model.selectedAPI == "" && !model.async.OperationCount.Loading {
			if _, ok := model.operationCatalogCount[key]; !ok {
				countCmd = model.ensureOperationCountLoadCmd()
			}
		}
		if countCmd == nil {
			return model, detailCmd
		}
		return model, batchCmds(detailCmd, countCmd)
	case operationDetailLoadedMsg:
		if !model.accepts(loadDomainOperationDetail, typed.Token) {
			return model, nil
		}
		model.finishLoad(loadDomainOperationDetail, typed.Token, nil)
		selected, ok := model.explorer.SelectedEndpoint()
		if !ok || selected.Identity != typed.Detail.Endpoint {
			return model, nil
		}
		detail := typed.Detail
		model.explorer.OperationCache[detail.Endpoint] = detail
		model.explorer.Detail.Operation = &detail
		model.refreshExplorerDetailViewport()
		return model, nil
	case specDetailLoadedMsg:
		if !model.accepts(loadDomainSpecDetail, typed.Token) {
			return model, nil
		}
		model.finishLoad(loadDomainSpecDetail, typed.Token, nil)
		selected, ok := model.explorer.SelectedEndpoint()
		if !ok {
			return model, nil
		}
		expected := selectedAPIIdentity(selected.Identity)
		received := selectedAPIIdentity(EndpointIdentity{
			Namespace: typed.Detail.Namespace,
			Repo:      typed.Detail.Repo,
			API:       typed.Detail.API,
		})
		if expected != received {
			return model, nil
		}
		detail := typed.Detail
		model.explorer.SpecCache[received] = detail
		model.explorer.Detail.Spec = &detail
		model.refreshExplorerDetailViewport()
	case apiSpecDetailLoadedMsg:
		if !model.accepts(loadDomainAPISpecDetail, typed.Token) {
			return model, nil
		}
		model.finishLoad(loadDomainAPISpecDetail, typed.Token, nil)
		selected, ok := model.selectedAPIEntry()
		if !ok {
			return model, nil
		}
		identity := SpecIdentity{
			Namespace: selected.Namespace,
			Repo:      selected.Repo,
			API:       selected.API,
		}
		received := SpecIdentity{
			Namespace: typed.Detail.Namespace,
			Repo:      typed.Detail.Repo,
			API:       typed.Detail.API,
		}
		if identity != received {
			return model, nil
		}
		detail := typed.Detail
		model.apiList.SpecCache[received] = detail
		model.apiList.Detail.Spec = &detail
		model.refreshAPIDetailViewport()
	case apiIssuesLoadedMsg:
		if !model.accepts(loadDomainAPIIssues, typed.Token) {
			return model, nil
		}
		model.finishLoad(loadDomainAPIIssues, typed.Token, nil)
		selected, ok := model.selectedAPIEntry()
		if !ok {
			return model, nil
		}
		expected := SpecIdentity{
			Namespace: selected.Namespace,
			Repo:      selected.Repo,
			API:       selected.API,
		}
		if expected != typed.Detail.API {
			return model, nil
		}
		detail := typed.Detail
		model.apiList.IssueCache[expected] = detail
		model.apiList.Detail.Issues = &detail
		model.refreshAPIDetailViewport()
	case loadFailedMsg:
		if !model.accepts(typed.Domain, typed.Token) {
			return model, nil
		}
		model.finishLoad(typed.Domain, typed.Token, typed.Err)
		switch typed.Domain {
		case loadDomainRepoCatalog:
			model.refreshRepoList()
		case loadDomainAPICatalog:
			model.refreshAPIList()
		case loadDomainNamespaces:
			model.refreshNamespaceList()
		case loadDomainNamespaceCount:
			model.home.Entries = withHomeNamespaceCountUnavailable(model.home.Entries)
			model.refreshHomeEntryPresentation()
			model.refreshHomeList()
		case loadDomainRepoCount, loadDomainAPICount, loadDomainOperationCount:
			model.resizeLists()
		case loadDomainOperationDetail:
			model.refreshExplorerDetailViewport()
		case loadDomainAPISpecDetail, loadDomainAPIIssues:
			model.refreshAPIDetailViewport()
		}
	}

	return model, nil
}

func (model *rootModel) shouldQuit(key string) bool {
	switch key {
	case "ctrl+c":
		return true
	case "q":
		return !model.activeListSettingFilter()
	default:
		return false
	}
}

func (model *rootModel) activeListSettingFilter() bool {
	switch model.activeRoute {
	case RouteHome:
		return model.home.List.SettingFilter()
	case RouteNamespaces:
		return model.namespaces.List.SettingFilter()
	case RouteRepos:
		return model.repoList.List.SettingFilter()
	case RouteAPIs:
		return model.apiList.List.SettingFilter()
	case RouteRepoExplorer:
		return model.explorer.List.SettingFilter()
	default:
		return false
	}
}

func (model *rootModel) updateActiveRouteList(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch model.activeRoute {
	case RouteHome:
		var cmd tea.Cmd
		model.home.List, cmd = model.home.List.Update(msg)
		model.syncHomeSelection()
		return model, model.batchWithActiveSectionEnsure(cmd)
	case RouteNamespaces:
		var cmd tea.Cmd
		beforeFilter := model.namespaces.List.FilterValue()
		model.namespaces.List, cmd = model.namespaces.List.Update(msg)
		model.syncNamespaceSelection()
		if beforeFilter != model.namespaces.List.FilterValue() {
			model.namespaces.Query = strings.TrimSpace(model.namespaces.List.FilterValue())
			model.namespaces.Pager.Page = 0
			model.namespaceCatalogHasMore = true
			return model, batchCmds(cmd, model.ensureNamespaceCountLoadCmd(), model.ensureNamespaceCatalogLoadCmd())
		}
		return model, batchCmds(cmd, model.ensureNamespaceCatalogLoadCmd())
	case RouteRepos:
		var cmd tea.Cmd
		beforeFilter := model.repoList.List.FilterValue()
		model.repoList.List, cmd = model.repoList.List.Update(msg)
		model.syncRepoSelection()
		if beforeFilter != model.repoList.List.FilterValue() {
			model.repoList.Query = strings.TrimSpace(model.repoList.List.FilterValue())
			model.repoList.Pager.Page = 0
			model.repoCatalogHasMore[model.selectedNamespace] = true
			return model, batchCmds(cmd, model.ensureRepoCountLoadCmd(), model.ensureRepoCatalogLoadCmd())
		}
		return model, batchCmds(cmd, model.ensureRepoCatalogLoadCmd())
	case RouteAPIs:
		var cmd tea.Cmd
		beforeFilter := model.apiList.List.FilterValue()
		model.apiList.List, cmd = model.apiList.List.Update(msg)
		detailCmd := model.syncAPISelection()
		if beforeFilter != model.apiList.List.FilterValue() {
			model.apiList.Query = strings.TrimSpace(model.apiList.List.FilterValue())
			model.apiList.Pager.Page = 0
			return model, batchCmds(cmd, detailCmd, model.ensureAPICountLoadCmd(), model.ensureAPICatalogLoadCmd())
		}
		return model, batchCmds(cmd, detailCmd, model.ensureAPICatalogLoadCmd())
	case RouteRepoExplorer:
		var cmd tea.Cmd
		beforeFilter := model.explorer.List.FilterValue()
		model.explorer.List, cmd = model.explorer.List.Update(msg)
		if beforeFilter != model.explorer.List.FilterValue() {
			model.explorer.Query = strings.TrimSpace(model.explorer.List.FilterValue())
			model.explorer.Pager.Page = 0
			model.endpointHasMoreByScope[model.endpointScopeKey()] = true
			return model, batchCmds(cmd, model.ensureOperationCountLoadCmd(), model.ensureEndpointCatalogLoadCmd())
		}
		return model, batchCmds(cmd, model.ensureEndpointCatalogLoadCmd())
	default:
		return model, nil
	}
}

func (model *rootModel) updateKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch model.activeRoute {
	case RouteHome:
		return model.updateHomeKey(msg)
	case RouteNamespaces:
		return model.updateNamespacesKey(msg)
	case RouteRepos:
		return model.updateReposKey(msg)
	case RouteAPIs:
		return model.updateAPIsKey(msg)
	case RouteRepoExplorer:
		return model.updateExplorerKey(msg)
	default:
		return model, nil
	}
}

func (model *rootModel) updateHomeKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "left", "shift+tab":
		model.setHomeSelection((model.home.Selected - 1 + len(model.home.Entries)) % len(model.home.Entries))
		return model, model.ensureLoadForActiveSection()
	case "right", "tab":
		model.setHomeSelection((model.home.Selected + 1) % len(model.home.Entries))
		return model, model.ensureLoadForActiveSection()
	case "enter":
		model.activeRoute = model.contextRouteFromHomeSelection()
		return model, model.ensureLoadForActiveSection()
	case "backspace":
		model.clearActiveSelection()
		return model, model.ensureLoadForActiveSection()
	default:
		return model, nil
	}
}

func (model *rootModel) updateNamespacesKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		if model.namespaces.List.SettingFilter() || model.namespaces.List.IsFiltered() {
			var cmd tea.Cmd
			model.namespaces.List, cmd = model.namespaces.List.Update(msg)
			model.syncNamespaceSelection()
			return model, cmd
		}
		model.activeRoute = RouteHome
		model.syncHomeSelection()
		return model, nil
	case "enter":
		if model.namespaces.List.SettingFilter() {
			var cmd tea.Cmd
			model.namespaces.List, cmd = model.namespaces.List.Update(msg)
			model.syncNamespaceSelection()
			return model, cmd
		}
		if !model.canEnterRepoList() {
			return model, nil
		}
		selection := model.namespaces.Entries[model.namespaces.Selected]
		model.setNamespaceSelection(selection.Namespace)
		model.setHomeSelection(homeItemRepos)
		model.activeRoute = RouteRepos
		return model, model.ensureLoadForActiveSection()
	default:
		pageBefore := model.namespaces.Pager.Page
		model.namespaces.Pager, _ = model.namespaces.Pager.Update(msg)
		if model.namespaces.Pager.Page != pageBefore {
			model.namespaceCatalogHasMore = true
			return model, model.ensureNamespaceCatalogLoadCmd()
		}
		var cmd tea.Cmd
		beforeFilter := model.namespaces.List.FilterValue()
		model.namespaces.List, cmd = model.namespaces.List.Update(msg)
		model.syncNamespaceSelection()
		if beforeFilter != model.namespaces.List.FilterValue() {
			model.namespaces.Query = strings.TrimSpace(model.namespaces.List.FilterValue())
			model.namespaces.Pager.Page = 0
			model.namespaceCatalogHasMore = true
			return model, batchCmds(cmd, model.ensureNamespaceCountLoadCmd(), model.ensureNamespaceCatalogLoadCmd())
		}
		return model, cmd
	}
}

func (model *rootModel) updateReposKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		model.activeRoute = RouteHome
		model.syncHomeSelection()
		return model, nil
	case "enter":
		if model.repoList.Selected < 0 || model.repoList.Selected >= len(model.repoList.Entries) {
			return model, nil
		}
		selected := model.repoList.Entries[model.repoList.Selected]
		model.setRepoSelection(selected.Namespace, selected.Repo)
		model.setHomeSelection(homeItemAPIs)
		model.activeRoute = RouteAPIs
		return model, model.ensureLoadForActiveSection()
	default:
		pageBefore := model.repoList.Pager.Page
		model.repoList.Pager, _ = model.repoList.Pager.Update(msg)
		if model.repoList.Pager.Page != pageBefore {
			model.repoCatalogHasMore[model.selectedNamespace] = true
			return model, model.ensureRepoCatalogLoadCmd()
		}
		var cmd tea.Cmd
		beforeFilter := model.repoList.List.FilterValue()
		model.repoList.List, cmd = model.repoList.List.Update(msg)
		model.syncRepoSelection()
		if beforeFilter != model.repoList.List.FilterValue() {
			model.repoList.Query = strings.TrimSpace(model.repoList.List.FilterValue())
			model.repoList.Pager.Page = 0
			model.repoCatalogHasMore[model.selectedNamespace] = true
			return model, batchCmds(cmd, model.ensureRepoCountLoadCmd(), model.ensureRepoCatalogLoadCmd())
		}
		return model, cmd
	}
}

func (model *rootModel) updateAPIsKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		model.activeRoute = RouteHome
		model.syncHomeSelection()
		return model, nil
	case "tab":
		return model, model.switchAPIDetailTab(1)
	case "shift+tab":
		return model, model.switchAPIDetailTab(-1)
	case "enter":
		if model.apiList.Selected < 0 || model.apiList.Selected >= len(model.apiList.Entries) {
			return model, nil
		}
		selected := model.apiList.Entries[model.apiList.Selected]
		model.setAPISelection(selected)
		model.setHomeSelection(homeItemEndpoints)
		model.activeRoute = RouteRepoExplorer
		return model, model.ensureLoadForActiveSection()
	default:
		pageBefore := model.apiList.Pager.Page
		model.apiList.Pager, _ = model.apiList.Pager.Update(msg)
		if model.apiList.Pager.Page != pageBefore {
			return model, batchCmds(model.ensureAPICatalogLoadCmd(), model.ensureAPIDetailLoadForTab())
		}
		var cmd tea.Cmd
		beforeFilter := model.apiList.List.FilterValue()
		model.apiList.List, cmd = model.apiList.List.Update(msg)
		detailSelectionCmd := model.syncAPISelection()
		if beforeFilter != model.apiList.List.FilterValue() {
			model.apiList.Query = strings.TrimSpace(model.apiList.List.FilterValue())
			model.apiList.Pager.Page = 0
			return model, batchCmds(cmd, detailSelectionCmd, model.ensureAPICountLoadCmd(), model.ensureAPICatalogLoadCmd())
		}
		var viewportCmd tea.Cmd
		if shouldRouteKeyToDetailViewport(msg) {
			model.apiList.Detail.Viewport, viewportCmd = model.apiList.Detail.Viewport.Update(msg)
		}
		return model, batchCmds(cmd, detailSelectionCmd, viewportCmd)
	}
}

func (model *rootModel) canEnterRepoList() bool {
	return model.namespaces.Selected >= 0 && model.namespaces.Selected < len(model.namespaces.Entries)
}

func (model *rootModel) refreshHomeList() {
	model.home.List.SetItems(homeItems(model.home.Entries))
	if len(model.home.Entries) == 0 {
		model.home.Selected = -1
		model.home.List.ResetSelected()
		return
	}
	if model.home.Selected < 0 || model.home.Selected >= len(model.home.Entries) {
		model.home.Selected = 0
	}
	model.home.List.Select(model.home.Selected)
}

func (model *rootModel) refreshNamespaceList() {
	filterValue := model.namespaces.List.FilterValue()
	filterState := model.namespaces.List.FilterState()
	model.namespaces.List.SetItems(namespaceItems(model.namespaces.Entries))
	if len(model.namespaces.Entries) == 0 {
		model.namespaces.Selected = -1
		model.namespaces.List.ResetSelected()
		restoreListFilter(&model.namespaces.List, filterValue, filterState)
		return
	}
	if model.namespaces.Selected < 0 || model.namespaces.Selected >= len(model.namespaces.Entries) {
		model.namespaces.Selected = 0
	}
	model.namespaces.List.Select(model.namespaces.Selected)
	restoreListFilter(&model.namespaces.List, filterValue, filterState)
}

func (model *rootModel) refreshRepoList() {
	filterValue := model.repoList.List.FilterValue()
	filterState := model.repoList.List.FilterState()
	model.repoList.Namespace = model.selectedNamespace
	model.repoList.Entries = repoEntriesByNamespace(model.repos, model.repoList.Namespace)
	model.repoList.List.Title = "REPOSITORIES"
	model.repoList.List.SetItems(repoItems(model.repoList.Entries))
	if len(model.repoList.Entries) == 0 {
		model.repoList.Selected = -1
		model.repoList.List.ResetSelected()
		restoreListFilter(&model.repoList.List, filterValue, filterState)
		return
	}
	if model.repoList.Selected < 0 || model.repoList.Selected >= len(model.repoList.Entries) {
		model.repoList.Selected = 0
	}
	model.repoList.List.Select(model.repoList.Selected)
	restoreListFilter(&model.repoList.List, filterValue, filterState)
}

func (model *rootModel) refreshAPIList() {
	filterValue := model.apiList.List.FilterValue()
	filterState := model.apiList.List.FilterState()
	model.apiList.Namespace = model.selectedNamespace
	model.apiList.Repo = model.selectedRepo
	model.apiList.Entries = model.filteredAPIEntries()
	model.apiList.List.Title = "APIS"
	model.apiList.List.SetItems(apiItems(model.apiList.Entries))

	if len(model.apiList.Entries) == 0 {
		model.apiList.Selected = -1
		model.apiList.List.ResetSelected()
		restoreListFilter(&model.apiList.List, filterValue, filterState)
		return
	}
	if model.selectedAPI != "" {
		for index, entry := range model.apiList.Entries {
			if entry.API == model.selectedAPI &&
				entry.Namespace == model.selectedNamespace &&
				entry.Repo == model.selectedRepo {
				model.apiList.Selected = index
				model.apiList.List.Select(index)
				restoreListFilter(&model.apiList.List, filterValue, filterState)
				return
			}
		}
	}
	if model.apiList.Selected < 0 || model.apiList.Selected >= len(model.apiList.Entries) {
		model.apiList.Selected = 0
	}
	model.apiList.List.Select(model.apiList.Selected)
	restoreListFilter(&model.apiList.List, filterValue, filterState)
}

func (model *rootModel) refreshExplorerList() {
	filterValue := model.explorer.List.FilterValue()
	filterState := model.explorer.List.FilterState()
	entries := model.filteredEndpointEntries()
	if len(entries) == 0 && len(model.endpointCatalogByRepo) == 0 && len(model.explorer.Endpoints) > 0 {
		entries = sortedEndpointEntries(model.explorer.Endpoints)
	}
	model.explorer.Endpoints = entries
	model.explorer.List.Title = "ENDPOINTS"
	model.explorer.List.SetItems(endpointItems(entries))
	if len(entries) == 0 {
		model.explorer.Selected = -1
		model.explorer.List.ResetSelected()
		restoreListFilter(&model.explorer.List, filterValue, filterState)
		return
	}
	if model.selectedEndpoint != nil {
		for index, entry := range entries {
			if entry.Identity == *model.selectedEndpoint {
				model.explorer.Selected = index
				model.explorer.List.Select(index)
				restoreListFilter(&model.explorer.List, filterValue, filterState)
				return
			}
		}
	}
	if model.explorer.Selected < 0 || model.explorer.Selected >= len(entries) {
		model.explorer.Selected = 0
	}
	model.explorer.List.Select(model.explorer.Selected)
	restoreListFilter(&model.explorer.List, filterValue, filterState)
}

func (model *rootModel) filteredEndpointEntries() []EndpointEntry {
	entries, ok := model.endpointCatalogByRepo[model.endpointScopeKey()]
	if !ok {
		return nil
	}
	return sortedEndpointEntries(entries)
}

func (model *rootModel) filteredAPIEntries() []APIEntry {
	entries, ok := model.apiCatalogByScope[apiScopeKey(model.selectedNamespace, model.selectedRepo)]
	if !ok {
		return nil
	}
	return append([]APIEntry(nil), entries...)
}

func (model *rootModel) syncNamespaceSelection() {
	index := model.namespaces.List.GlobalIndex()
	if len(model.namespaces.Entries) == 0 || index < 0 || index >= len(model.namespaces.Entries) {
		model.namespaces.Selected = -1
		return
	}
	model.namespaces.Selected = index
}

func restoreListFilter(model *list.Model, value string, state list.FilterState) {
	trimmed := strings.TrimSpace(value)
	if trimmed != "" {
		model.SetFilterText(value)
	}
	model.SetFilterState(state)
}

func (model *rootModel) syncHomeSelection() {
	index := model.home.List.Index()
	if len(model.home.Entries) == 0 || index < 0 || index >= len(model.home.Entries) {
		model.home.Selected = -1
		return
	}
	model.home.Selected = index
}

func (model *rootModel) syncRepoSelection() {
	index := model.repoList.List.Index()
	if len(model.repoList.Entries) == 0 || index < 0 || index >= len(model.repoList.Entries) {
		model.repoList.Selected = -1
		return
	}
	model.repoList.Selected = index
}

func (model *rootModel) syncAPISelection() tea.Cmd {
	index := model.apiList.List.Index()
	if len(model.apiList.Entries) == 0 || index < 0 || index >= len(model.apiList.Entries) {
		previous := model.apiList.Selected
		model.apiList.Selected = -1
		if previous >= 0 {
			model.clearAPIDetailState()
			model.refreshAPIDetailViewport()
		}
		return nil
	}
	previous := model.apiList.Selected
	model.apiList.Selected = index
	if previous != index {
		model.clearAPIDetailState()
		model.refreshAPIDetailViewport()
		return model.ensureAPIDetailLoadForTab()
	}
	return nil
}

func (model *rootModel) resizeLists() {
	width, height := listSize(model.width, model.height)
	listWidth := model.activeListWidth(width)
	// Reserve rows for header+gap and bottom paginator/help shell.
	listHeight := height - 5
	if model.home.Selected == homeItemEndpoints {
		// Endpoints screen renders its own split body; keep it flush with available body height.
		listHeight = height - 4
	}
	if listHeight < 1 {
		listHeight = 1
	}
	model.home.List.SetSize(listWidth, listHeight)
	model.namespaces.List.SetSize(listWidth, listHeight)
	model.repoList.List.SetSize(listWidth, listHeight)
	model.apiList.List.SetSize(listWidth, listHeight)
	model.explorer.List.SetSize(listWidth, listHeight)
	model.help.SetWidth(width)

	detailWidth := width - listWidth - 2
	if model.home.Selected == homeItemEndpoints {
		detailWidth = endpointDetailsTargetWidth(width)
		if listWidth+2+detailWidth > width {
			detailWidth = listWidth
		}
	} else if detailWidth < 24 {
		detailWidth = listWidth
	}
	model.explorer.Detail.Viewport.SetWidth(detailWidth)
	model.apiList.Detail.Viewport.SetWidth(detailWidth)
	detailHeight := listHeight - 3
	if model.home.Selected == homeItemEndpoints {
		// Tabs and spacer are rendered outside the viewport.
		detailHeight = height - 5
	}
	if detailHeight < 1 {
		detailHeight = 1
	}
	model.explorer.Detail.Viewport.SetHeight(detailHeight)
	model.apiList.Detail.Viewport.SetHeight(detailHeight)
}

func (model *rootModel) activeListWidth(terminalWidth int) int {
	scopeWidth := defaultListWidth
	switch model.home.Selected {
	case homeItemNamespaces:
		scopeWidth = measuredListWidth(model.namespaceCatalogCount.MaxItemLength)
	case homeItemRepos:
		scopeWidth = measuredListWidth(model.repoCatalogCount[model.selectedNamespace].MaxItemLength)
	case homeItemAPIs:
		scopeWidth = measuredListWidth(model.apiCatalogCount[apiScopeKey(model.selectedNamespace, model.selectedRepo)].MaxItemLength)
	case homeItemEndpoints:
		scopeWidth = measuredListWidth(model.operationCatalogCount[repoPath(model.selectedNamespace, model.selectedRepo)].MaxItemLength)
	}
	if scopeWidth > terminalWidth {
		return terminalWidth
	}
	return scopeWidth
}

func measuredListWidth(maxLength int64) int {
	if maxLength < int64(defaultListWidth) {
		return defaultListWidth
	}
	return int(maxLength)
}

func (model *rootModel) refreshExplorerDetailViewportIfVisible() {
	if model.home.Selected == homeItemEndpoints {
		model.refreshExplorerDetailViewport()
	}
}

func (model *rootModel) refreshAPIDetailViewportIfVisible() {
	if model.home.Selected == homeItemAPIs {
		model.refreshAPIDetailViewport()
	}
}

func (model *rootModel) View() tea.View {
	s := model.viewBrowser()
	v := tea.NewView(s)
	v.AltScreen = true
	return v
}

func (model *rootModel) refreshListTitleStyles() {
	// List titles are hidden; header carries active/focus styling.
}

func (model *rootModel) focusedListModel() *list.Model {
	switch model.activeRoute {
	case RouteNamespaces:
		return &model.namespaces.List
	case RouteRepos:
		return &model.repoList.List
	case RouteAPIs:
		return &model.apiList.List
	case RouteRepoExplorer:
		return &model.explorer.List
	case RouteHome:
		fallthrough
	default:
		return &model.home.List
	}
}

func (model *rootModel) viewBrowser() string {
	header := model.headerView()
	contextPane := model.activeContextPaneView()
	footer := model.routeHelpView()

	if model.home.Selected == homeItemAPIs {
		return model.viewAPIsScreen(header, footer)
	}
	if model.home.Selected == homeItemEndpoints {
		return model.viewEndpointsScreen(header, footer)
	}

	body := contextPane
	if model.async.NamespaceCount.LastError != nil {
		body = strings.Join([]string{
			body,
			"",
			model.styles.ErrorBlock(
				"Failed to load namespace total.",
				model.async.NamespaceCount.LastError.Error(),
			),
		}, "\n")
	}
	return model.layoutScreen(strings.Join([]string{header, "", body}, "\n"), footer)
}

func (model *rootModel) viewEndpointsScreen(header string, footer string) string {
	left := strings.Join([]string{
		header,
		"",
		model.viewEndpointsPane(),
	}, "\n")
	right := model.endpointDetailsPaneView()

	width, _ := listSize(model.width, model.height)
	leftWidth := model.activeListWidth(width)
	rightWidth := endpointDetailsTargetWidth(width)
	stacked := leftWidth+2+rightWidth > width

	body := ""
	if stacked {
		body = strings.Join([]string{
			renderPaneAtWidth(left, leftWidth),
			"",
			renderPaneAtWidth(right, leftWidth),
		}, "\n")
	} else {
		body = lipgloss.JoinHorizontal(
			lipgloss.Top,
			renderPaneAtWidth(left, leftWidth),
			"  ",
			renderPaneAtWidth(right, rightWidth),
		)
	}

	if model.async.NamespaceCount.LastError != nil {
		body = strings.Join([]string{
			body,
			"",
			model.styles.ErrorBlock(
				"Failed to load namespace total.",
				model.async.NamespaceCount.LastError.Error(),
			),
		}, "\n")
	}

	return model.layoutScreen(body, footer)
}

func (model *rootModel) viewAPIsScreen(header string, footer string) string {
	left := strings.Join([]string{
		header,
		"",
		model.viewAPIsPane(),
	}, "\n")
	right := model.apiDetailsPaneView()

	width, _ := listSize(model.width, model.height)
	leftWidth := model.activeListWidth(width)
	rightWidth := endpointDetailsTargetWidth(width)
	stacked := leftWidth+2+rightWidth > width

	body := ""
	if stacked {
		body = strings.Join([]string{
			renderPaneAtWidth(left, leftWidth),
			"",
			renderPaneAtWidth(right, leftWidth),
		}, "\n")
	} else {
		body = lipgloss.JoinHorizontal(
			lipgloss.Top,
			renderPaneAtWidth(left, leftWidth),
			"  ",
			renderPaneAtWidth(right, rightWidth),
		)
	}

	if model.async.NamespaceCount.LastError != nil {
		body = strings.Join([]string{
			body,
			"",
			model.styles.ErrorBlock(
				"Failed to load namespace total.",
				model.async.NamespaceCount.LastError.Error(),
			),
		}, "\n")
	}

	return model.layoutScreen(body, footer)
}

func (model *rootModel) headerView() string {
	brand := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252")).
		Faint(true).
		Render("SHIVA")

	items := []string{"NAMESPACES", "REPOS", "APIS", "ENDPOINTS"}
	segments := make([]string, 0, len(items))
	headerFocused := model.activeRoute == RouteHome
	for index, item := range items {
		style := lipgloss.NewStyle().Padding(0, 1)
		if model.home.Selected == index {
			style = style.Foreground(lipgloss.Color("230"))
			if headerFocused {
				style = style.Background(lipgloss.Color("62"))
			}
		} else {
			style = style.Faint(true)
		}
		segments = append(segments, style.Render(item))
	}

	return strings.Join([]string{brand, strings.Join(segments, " / ")}, " :// ")
}

func renderPaneAtWidth(view string, width int) string {
	if width <= 0 {
		return view
	}
	return lipgloss.NewStyle().Width(width).Render(view)
}

func joinPanes(panes []string) []string {
	joined := make([]string, 0, len(panes)*2)
	for idx, pane := range panes {
		if idx > 0 {
			joined = append(joined, "  ")
		}
		joined = append(joined, pane)
	}
	return joined
}

func (model *rootModel) activeContextPaneView() string {
	switch model.home.Selected {
	case homeItemNamespaces:
		return model.viewNamespacesPane()
	case homeItemRepos:
		return model.viewReposPane()
	case homeItemAPIs:
		return model.viewAPIsPane()
	case homeItemEndpoints:
		return model.viewEndpointsPane()
	default:
		return model.styles.EmptyBlock("No section selected.")
	}
}

func (model *rootModel) viewNamespacesPane() string {
	if model.async.Namespaces.Loading && len(model.namespaces.Entries) == 0 {
		return model.styles.EmptyBlock("Loading namespaces...")
	}
	if model.async.Namespaces.LastError != nil {
		return model.styles.ErrorBlock(
			"Failed to load namespaces.",
			model.async.Namespaces.LastError.Error(),
		)
	}
	if len(model.namespaces.Entries) == 0 {
		return model.styles.EmptyBlock("No namespaces found.")
	}
	return model.namespaces.List.View()
}

func (model *rootModel) viewReposPane() string {
	if model.async.RepoCatalog.Loading && len(model.repos) == 0 {
		return model.styles.EmptyBlock("Loading repositories...")
	}
	if model.async.RepoCatalog.LastError != nil {
		return model.styles.ErrorBlock(
			"Failed to load repositories.",
			model.async.RepoCatalog.LastError.Error(),
		)
	}
	if len(model.repoList.Entries) == 0 {
		if model.selectedNamespace == "" {
			return model.styles.EmptyBlock("No repositories found.")
		}
		return model.styles.EmptyBlock("No repositories found in namespace.")
	}
	return model.repoList.List.View()
}

func (model *rootModel) viewAPIsPane() string {
	if model.async.APICatalog.Loading && len(model.apiList.Entries) == 0 {
		return model.styles.EmptyBlock("Loading APIs...")
	}
	if model.async.APICatalog.LastError != nil {
		return model.styles.ErrorBlock(
			"Failed to load APIs.",
			model.async.APICatalog.LastError.Error(),
		)
	}
	if len(model.apiList.Entries) == 0 {
		return model.styles.EmptyBlock("No APIs found for current scope.")
	}
	return model.apiList.List.View()
}

func (model *rootModel) viewEndpointsPane() string {
	if model.async.OperationList.Loading && len(model.explorer.Endpoints) == 0 {
		return model.styles.EmptyBlock("Loading endpoints...")
	}
	if len(model.explorer.Endpoints) == 0 {
		if model.async.OperationList.LastError != nil {
			return model.styles.ErrorBlock(
				"Failed to load endpoints.",
				model.async.OperationList.LastError.Error(),
			)
		}
		return model.styles.EmptyBlock("No endpoints found for current scope.")
	}
	return model.explorer.List.View()
}

func (model *rootModel) endpointDetailsPaneView() string {
	body := strings.Join([]string{
		model.explorerTabRow(),
		"",
		model.explorer.Detail.Viewport.View(),
	}, "\n")
	return model.styles.DetailPane(body, model.explorer.Detail.Viewport.Width())
}

func (model *rootModel) apiDetailsPaneView() string {
	body := strings.Join([]string{
		model.apiTabRow(),
		"",
		model.apiList.Detail.Viewport.View(),
	}, "\n")
	return model.styles.DetailPane(body, model.apiList.Detail.Viewport.Width())
}

func endpointDetailsTargetWidth(viewportWidth int) int {
	width := viewportWidth / 3
	if width < 90 {
		width = 90
	}
	return width
}

func browserPaneLayout(width int, includeDetails bool) (int, int, int, bool) {
	width, _ = listSize(width, defaultViewportHeight)
	const paneGap = 2
	if !includeDetails {
		if width < defaultListWidth*2+paneGap {
			return width, width, 0, true
		}
		left := defaultListWidth
		center := width - left - paneGap
		if center < defaultListWidth {
			return width, width, 0, true
		}
		return left, center, 0, false
	}
	const detailMinWidth = 28
	if width < defaultListWidth*2+detailMinWidth+paneGap*2 {
		return width, width, width, true
	}
	left := defaultListWidth
	center := defaultListWidth
	right := width - left - center - paneGap*2
	if right < detailMinWidth {
		return width, width, width, true
	}
	return left, center, right, false
}

func (model *rootModel) layoutScreen(body string, footer string) string {
	paginatorLine := model.activePaginatorLine()
	paginatorView := model.styles.Subtle(paginatorLine)
	if model.height <= 0 {
		return strings.Join([]string{body, "", paginatorView, footer}, "\n")
	}
	maxBodyHeight := model.height - lipgloss.Height(paginatorLine) - lipgloss.Height(footer)
	if maxBodyHeight < 1 {
		maxBodyHeight = 1
	}
	body = clampRenderHeight(body, maxBodyHeight)

	separatorNewlines := model.height - lipgloss.Height(body) - lipgloss.Height(paginatorLine) - lipgloss.Height(footer) + 1
	if separatorNewlines < 1 {
		separatorNewlines = 1
	}

	return body + strings.Repeat("\n", separatorNewlines) + paginatorView + "\n" + footer
}

func clampRenderHeight(view string, maxHeight int) string {
	if maxHeight < 1 || lipgloss.Height(view) <= maxHeight {
		return view
	}
	lines := strings.Split(view, "\n")
	if len(lines) <= maxHeight {
		return view
	}
	return strings.Join(lines[:maxHeight], "\n")
}

func (model *rootModel) setHomeSelection(index int) {
	if index < 0 {
		index = 0
	}
	if len(model.home.Entries) == 0 {
		model.home.Selected = -1
		model.home.List.ResetSelected()
		return
	}
	if index >= len(model.home.Entries) {
		index = len(model.home.Entries) - 1
	}
	model.home.Selected = index
	model.home.List.Select(index)
	model.resizeLists()
}

func (model *rootModel) contextRouteFromHomeSelection() RouteKind {
	switch model.home.Selected {
	case homeItemNamespaces:
		return RouteNamespaces
	case homeItemRepos:
		return RouteRepos
	case homeItemAPIs:
		return RouteAPIs
	case homeItemEndpoints:
		return RouteRepoExplorer
	default:
		return RouteNamespaces
	}
}

func (model *rootModel) clearActiveSelection() {
	switch model.home.Selected {
	case homeItemNamespaces:
		model.selectedNamespace = ""
		model.selectedRepo = ""
		model.selectedAPI = ""
		model.selectedEndpoint = nil
	case homeItemRepos:
		model.selectedRepo = ""
		model.selectedAPI = ""
		model.selectedEndpoint = nil
	case homeItemAPIs:
		model.selectedAPI = ""
		model.selectedEndpoint = nil
		model.clearAPIDetailState()
	case homeItemEndpoints:
		model.selectedEndpoint = nil
	}
	model.refreshHomeEntryPresentation()
	model.refreshHomeList()
	model.refreshRepoList()
	model.refreshAPIList()
	model.refreshExplorerList()
	model.refreshAPIDetailViewport()
	model.refreshExplorerDetailViewport()
}

func (model *rootModel) setNamespaceSelection(namespace string) {
	if model.selectedNamespace != namespace {
		model.selectedRepo = ""
		model.selectedAPI = ""
		model.selectedEndpoint = nil
		model.clearAPIDetailState()
		model.repoList.Pager.Page = 0
		model.apiList.Pager.Page = 0
		model.explorer.Pager.Page = 0
	}
	model.selectedNamespace = namespace
	model.explorer.Namespace = namespace
	model.explorer.Repo = ""
	model.apiList.Namespace = namespace
	model.apiList.Repo = ""
	model.refreshHomeEntryPresentation()
	model.refreshHomeList()
	model.refreshRepoList()
	model.refreshAPIList()
	model.refreshExplorerList()
	model.refreshAPIDetailViewport()
	model.refreshExplorerDetailViewport()
}

func (model *rootModel) setRepoSelection(namespace string, repo string) {
	namespaceChanged := model.selectedNamespace != namespace
	repoChanged := model.selectedRepo != repo
	model.selectedNamespace = namespace
	model.selectedRepo = repo
	model.selectedAPI = ""
	model.explorer.Namespace = namespace
	model.explorer.Repo = repo
	model.apiList.Namespace = namespace
	model.apiList.Repo = repo
	if namespaceChanged || repoChanged {
		model.selectedEndpoint = nil
		model.clearAPIDetailState()
		model.apiList.Pager.Page = 0
		model.explorer.Pager.Page = 0
	}
	model.refreshHomeEntryPresentation()
	model.refreshHomeList()
	model.refreshRepoList()
	model.refreshAPIList()
	model.refreshExplorerList()
	model.refreshAPIDetailViewport()
	model.refreshExplorerDetailViewport()
}

func (model *rootModel) setAPISelection(entry APIEntry) {
	api := strings.TrimSpace(entry.API)
	namespace := strings.TrimSpace(entry.Namespace)
	repo := strings.TrimSpace(entry.Repo)
	if model.selectedAPI != api || model.selectedNamespace != namespace || model.selectedRepo != repo {
		model.selectedEndpoint = nil
		model.clearAPIDetailState()
		model.explorer.Pager.Page = 0
	}
	if namespace != "" {
		model.selectedNamespace = namespace
	}
	if repo != "" {
		model.selectedRepo = repo
	}
	model.selectedAPI = api
	model.explorer.Namespace = model.selectedNamespace
	model.explorer.Repo = model.selectedRepo
	model.apiList.Namespace = model.selectedNamespace
	model.apiList.Repo = model.selectedRepo
	model.refreshHomeEntryPresentation()
	model.refreshHomeList()
	model.refreshRepoList()
	model.refreshAPIList()
	model.refreshExplorerList()
	model.refreshAPIDetailViewport()
	model.refreshExplorerDetailViewport()
}

func (model *rootModel) setEndpointSelection(identity EndpointIdentity) {
	id := identity
	model.selectedEndpoint = &id
	model.selectedNamespace = identity.Namespace
	model.selectedRepo = identity.Repo
	model.selectedAPI = identity.API
	model.explorer.Namespace = identity.Namespace
	model.explorer.Repo = identity.Repo
	model.apiList.Namespace = identity.Namespace
	model.apiList.Repo = identity.Repo
	model.refreshHomeEntryPresentation()
	model.refreshHomeList()
	model.refreshRepoList()
	model.refreshAPIList()
	model.refreshExplorerList()
	model.refreshAPIDetailViewport()
	model.refreshExplorerDetailViewport()
}

func (model *rootModel) refreshHomeEntryPresentation() {
	if len(model.home.Entries) < 4 {
		model.home.Entries = defaultHomeEntries()
	}

	if model.selectedNamespace != "" {
		model.home.Entries[homeItemNamespaces].Title = model.selectedNamespace
	} else {
		model.home.Entries[homeItemNamespaces].Title = "Namespaces"
	}

	if model.selectedRepo != "" {
		model.home.Entries[homeItemRepos].Title = model.selectedRepo
	} else {
		model.home.Entries[homeItemRepos].Title = "Repos"
	}
	if model.selectedNamespace != "" {
		model.home.Entries[homeItemRepos].Description = model.selectedNamespace
	} else {
		model.home.Entries[homeItemRepos].Description = "Total: ..."
	}

	if model.selectedAPI != "" {
		model.home.Entries[homeItemAPIs].Title = model.selectedAPI
	} else {
		model.home.Entries[homeItemAPIs].Title = "APIs"
	}
	if model.selectedRepo != "" {
		model.home.Entries[homeItemAPIs].Description = model.selectedRepo
	} else if model.selectedNamespace != "" {
		model.home.Entries[homeItemAPIs].Description = model.selectedNamespace
	} else {
		model.home.Entries[homeItemAPIs].Description = "All repos"
	}

	if model.selectedEndpoint != nil {
		model.home.Entries[homeItemEndpoints].Title = endpointSelectionTitle(*model.selectedEndpoint)
	} else {
		model.home.Entries[homeItemEndpoints].Title = "Endpoints"
	}
	if model.selectedAPI != "" {
		model.home.Entries[homeItemEndpoints].Description = model.selectedAPI
	} else if model.selectedRepo != "" {
		model.home.Entries[homeItemEndpoints].Description = model.selectedRepo
	} else {
		model.home.Entries[homeItemEndpoints].Description = "Select api"
	}
}

func endpointSelectionTitle(identity EndpointIdentity) string {
	method := strings.ToUpper(strings.TrimSpace(identity.Method))
	path := strings.TrimSpace(identity.Path)
	if method != "" && path != "" {
		return method + " " + path
	}
	if identity.OperationID != "" {
		return "#" + identity.OperationID
	}
	if path != "" {
		return path
	}
	return "Endpoints"
}

func repoPath(namespace string, repo string) string {
	return namespace + "/" + repo
}

func apiScopeKey(namespace string, repo string) string {
	namespace = strings.TrimSpace(namespace)
	repo = strings.TrimSpace(repo)
	if namespace == "" && repo == "" {
		return "/"
	}
	if repo == "" {
		return namespace + "/"
	}
	return repoPath(namespace, repo)
}

func (model *rootModel) beginRepoCatalogLoad() RequestToken {
	return model.beginLoad(loadDomainRepoCatalog)
}

func (model *rootModel) beginNamespaceCountLoad() RequestToken {
	return model.beginLoad(loadDomainNamespaceCount)
}

func (model *rootModel) beginRepoCountLoad() RequestToken {
	return model.beginLoad(loadDomainRepoCount)
}

func (model *rootModel) beginAPICountLoad() RequestToken {
	return model.beginLoad(loadDomainAPICount)
}

func (model *rootModel) beginOperationCountLoad() RequestToken {
	return model.beginLoad(loadDomainOperationCount)
}

func (model *rootModel) beginNamespaceCatalogLoad() RequestToken {
	return model.beginLoad(loadDomainNamespaces)
}

func (model *rootModel) beginAPICatalogLoad() RequestToken {
	return model.beginLoad(loadDomainAPICatalog)
}

func (model *rootModel) ensureNamespaceCatalogLoadCmd() tea.Cmd {
	if model.async.Namespaces.Loading {
		return nil
	}
	if !model.namespaceCatalogHasMore {
		return nil
	}
	token := model.beginNamespaceCatalogLoad()
	page := model.options
	pageSize := int32(model.namespaceItemsPerPage())
	page.Limit = pageSize
	page.Offset = int32(model.namespaces.Pager.Page) * pageSize
	page.Query = model.namespaces.Query
	return loadNamespaceCatalogCmd(context.Background(), model.service, page, page.Offset, token)
}

func (model *rootModel) ensureNamespaceCountLoadCmd() tea.Cmd {
	if model.async.NamespaceCount.Loading {
		return nil
	}
	token := model.beginNamespaceCountLoad()
	options := model.options
	options.Query = model.namespaces.Query
	return loadNamespaceCountCmd(context.Background(), model.service, options, token)
}

func (model *rootModel) ensureRepoCountLoadCmd() tea.Cmd {
	token := model.beginRepoCountLoad()
	options := model.options
	options.Query = model.repoList.Query
	return loadRepoCountCmd(context.Background(), model.service, model.selectedNamespace, options, token)
}

func (model *rootModel) ensureAPICountLoadCmd() tea.Cmd {
	if model.async.APICount.Loading {
		return nil
	}
	token := model.beginAPICountLoad()
	options := model.options
	options.Query = model.apiList.Query
	return loadAPICountCmd(context.Background(), model.service, request.Envelope{
		Namespace: model.selectedNamespace,
		Repo:      model.selectedRepo,
	}, options, token)
}

func (model *rootModel) ensureOperationCountLoadCmd() tea.Cmd {
	if model.selectedAPI != "" {
		return nil
	}
	token := model.beginOperationCountLoad()
	options := model.options
	options.Query = model.explorer.Query
	return loadOperationCountCmd(context.Background(), model.service, request.Envelope{
		Namespace: model.selectedNamespace,
		Repo:      model.selectedRepo,
	}, options, token)
}

func (model *rootModel) ensureRepoCatalogLoadCmd() tea.Cmd {
	if model.async.RepoCatalog.Loading {
		return nil
	}
	if hasMore, ok := model.repoCatalogHasMore[model.selectedNamespace]; ok && !hasMore {
		return nil
	}
	token := model.beginRepoCatalogLoad()
	page := model.options
	pageSize := int32(model.repoItemsPerPage())
	page.Limit = pageSize
	page.Offset = int32(model.repoList.Pager.Page) * pageSize
	page.Query = model.repoList.Query
	page.Namespace = model.selectedNamespace
	return loadRepoCatalogCmd(context.Background(), model.service, page, page.Offset, token)
}

func (model *rootModel) ensureAPICatalogLoadCmd() tea.Cmd {
	if model.async.APICatalog.Loading {
		return nil
	}
	token := model.beginAPICatalogLoad()
	page := model.options
	pageSize := int32(model.apiItemsPerPage())
	page.Limit = pageSize
	page.Offset = int32(model.apiList.Pager.Page) * pageSize
	page.Query = model.apiList.Query
	return loadAPICatalogCmd(
		context.Background(),
		model.service,
		request.Envelope{
			Namespace: model.selectedNamespace,
			Repo:      model.selectedRepo,
		},
		page,
		token,
	)
}

func (model *rootModel) ensureEndpointCatalogLoadCmd() tea.Cmd {
	key := model.endpointScopeKey()
	if model.async.OperationList.Loading {
		return nil
	}
	if hasMore, ok := model.endpointHasMoreByScope[key]; ok && !hasMore {
		return nil
	}
	if model.selectedRepo != "" {
		for _, repo := range model.repos {
			if repo.Namespace == model.selectedNamespace && repo.Repo == model.selectedRepo &&
				repo.Row.SnapshotRevision == nil && repo.Row.ActiveAPICount == 0 {
				return nil
			}
		}
	}

	token := model.beginOperationListLoad()
	page := model.options
	pageSize := int32(model.endpointItemsPerPage())
	page.Limit = pageSize
	page.Offset = int32(model.explorer.Pager.Page) * pageSize
	page.Query = model.explorer.Query
	selector := request.Envelope{
		Namespace: model.selectedNamespace,
		Repo:      model.selectedRepo,
	}
	if model.selectedAPI != "" {
		selector.API = model.selectedAPI
	}
	return loadOperationListCmd(context.Background(), model.service, selector, page, page.Offset, token)
}

func (model *rootModel) endpointScopeKey() string {
	if model.selectedNamespace == "" && model.selectedRepo == "" {
		return "/"
	}
	if strings.TrimSpace(model.selectedAPI) == "" {
		return repoPath(model.selectedNamespace, model.selectedRepo)
	}
	return repoPath(model.selectedNamespace, model.selectedRepo) + "#" + strings.TrimSpace(model.selectedAPI)
}

func (model *rootModel) syncPaginator(pager *paginator.Model, totalCount int64, perPage int) {
	if perPage < 1 {
		perPage = 1
	}
	pager.PerPage = perPage
	totalPages := int((totalCount + int64(perPage) - 1) / int64(perPage))
	if totalPages < 1 {
		totalPages = 1
	}
	pager.TotalPages = totalPages
	if pager.Page >= totalPages {
		pager.Page = totalPages - 1
	}
}

func (model *rootModel) namespaceItemsPerPage() int {
	perPage := model.namespaces.List.Paginator.PerPage
	if perPage < 1 {
		return 1
	}
	return perPage
}

func (model *rootModel) repoItemsPerPage() int {
	perPage := model.repoList.List.Paginator.PerPage
	if perPage < 1 {
		return 1
	}
	return perPage
}

func (model *rootModel) endpointItemsPerPage() int {
	perPage := model.explorer.List.Paginator.PerPage
	if perPage < 1 {
		return 1
	}
	return perPage
}

func (model *rootModel) apiItemsPerPage() int {
	perPage := model.apiList.List.Paginator.PerPage
	if perPage < 1 {
		return 1
	}
	return perPage
}

func (model *rootModel) activePageSizeChanged(
	previousNamespacePerPage int,
	previousRepoPerPage int,
	previousSpecPerPage int,
	previousEndpointPerPage int,
) bool {
	switch model.home.Selected {
	case homeItemNamespaces:
		return previousNamespacePerPage != model.namespaceItemsPerPage()
	case homeItemRepos:
		return previousRepoPerPage != model.repoItemsPerPage()
	case homeItemAPIs:
		return previousSpecPerPage != model.apiItemsPerPage()
	case homeItemEndpoints:
		return previousEndpointPerPage != model.endpointItemsPerPage()
	default:
		return false
	}
}

func (model *rootModel) reloadActiveCatalogForResize() tea.Cmd {
	switch model.home.Selected {
	case homeItemNamespaces:
		model.namespaceCatalogHasMore = true
		return model.ensureNamespaceCatalogLoadCmd()
	case homeItemRepos:
		model.repoCatalogHasMore[model.selectedNamespace] = true
		return model.ensureRepoCatalogLoadCmd()
	case homeItemAPIs:
		return batchCmds(model.ensureAPICountLoadCmd(), model.ensureAPICatalogLoadCmd())
	case homeItemEndpoints:
		model.endpointHasMoreByScope[model.endpointScopeKey()] = true
		return model.ensureEndpointCatalogLoadCmd()
	default:
		return nil
	}
}

func (model *rootModel) syncKnownPaginators() {
	model.syncPaginator(
		&model.namespaces.Pager,
		model.namespaceCatalogCount.TotalCount,
		model.namespaceItemsPerPage(),
	)
	model.syncPaginator(
		&model.repoList.Pager,
		model.repoCatalogCount[model.selectedNamespace].TotalCount,
		model.repoItemsPerPage(),
	)
	model.syncPaginator(
		&model.apiList.Pager,
		model.apiCatalogCount[apiScopeKey(model.selectedNamespace, model.selectedRepo)].TotalCount,
		model.apiItemsPerPage(),
	)
	model.syncPaginator(
		&model.explorer.Pager,
		model.operationCatalogCount[repoPath(model.selectedNamespace, model.selectedRepo)].TotalCount,
		model.endpointItemsPerPage(),
	)
}

func (model *rootModel) activePaginatorView() string {
	return model.styles.Subtle(model.activePaginatorLine())
}

func (model *rootModel) activePaginatorLine() string {
	var pager paginator.Model
	switch model.home.Selected {
	case homeItemNamespaces:
		pager = model.namespaces.Pager
	case homeItemRepos:
		pager = model.repoList.Pager
	case homeItemAPIs:
		if model.async.APICount.Loading {
			return "..."
		}
		if _, ok := model.apiCatalogCount[apiScopeKey(model.selectedNamespace, model.selectedRepo)]; !ok {
			return "..."
		}
		pager = model.apiList.Pager
	case homeItemEndpoints:
		if model.async.OperationCount.Loading {
			return "..."
		}
		if _, ok := model.operationCatalogCount[repoPath(model.selectedNamespace, model.selectedRepo)]; !ok {
			return "..."
		}
		pager = model.explorer.Pager
	default:
		return ""
	}
	return pager.View()
}

func (model *rootModel) ensureLoadForActiveSection() tea.Cmd {
	switch model.home.Selected {
	case homeItemNamespaces:
		return batchCmds(model.ensureNamespaceCatalogLoadCmd(), model.ensureNamespaceCountLoadCmd())
	case homeItemRepos:
		return batchCmds(model.ensureNamespaceCatalogLoadCmd(), model.ensureRepoCatalogLoadCmd(), model.ensureRepoCountLoadCmd())
	case homeItemAPIs:
		return batchCmds(
			model.ensureRepoCatalogLoadCmd(),
			model.ensureAPICountLoadCmd(),
			model.ensureAPICatalogLoadCmd(),
			model.ensureAPIDetailLoadForTab(),
		)
	case homeItemEndpoints:
		return batchCmds(model.ensureRepoCatalogLoadCmd(), model.ensureAPICountLoadCmd(), model.ensureAPICatalogLoadCmd(), model.ensureEndpointCatalogLoadCmd())
	default:
		return nil
	}
}

func (model *rootModel) batchWithActiveSectionEnsure(cmd tea.Cmd) tea.Cmd {
	return batchCmds(cmd, model.ensureLoadForActiveSection())
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

func (model *rootModel) beginAPISpecDetailLoad() RequestToken {
	return model.beginLoad(loadDomainAPISpecDetail)
}

func (model *rootModel) beginAPIIssuesLoad() RequestToken {
	return model.beginLoad(loadDomainAPIIssues)
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
	case loadDomainNamespaceCount:
		return &model.async.NamespaceCount
	case loadDomainRepoCount:
		return &model.async.RepoCount
	case loadDomainAPICount:
		return &model.async.APICount
	case loadDomainOperationCount:
		return &model.async.OperationCount
	case loadDomainNamespaces:
		return &model.async.Namespaces
	case loadDomainRepoCatalog:
		return &model.async.RepoCatalog
	case loadDomainAPICatalog:
		return &model.async.APICatalog
	case loadDomainOperationList:
		return &model.async.OperationList
	case loadDomainOperationDetail:
		return &model.async.OperationDetail
	case loadDomainSpecDetail:
		return &model.async.SpecDetail
	case loadDomainAPISpecDetail:
		return &model.async.APISpecDetail
	case loadDomainAPIIssues:
		return &model.async.APIIssues
	default:
		panic(fmt.Sprintf("unsupported load domain %q", domain))
	}
}
