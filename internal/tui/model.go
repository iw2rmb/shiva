package tui

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const (
	homeItemNamespaces = 0
	homeItemRepos      = 1
	homeItemEndpoints  = 2

	endpointFanoutConcurrency = 4
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
	explorer     RepoExplorerRouteState
	async        AsyncState
	width        int
	height       int

	selectedNamespace string
	selectedRepo      string
	selectedEndpoint  *EndpointIdentity

	endpointCatalogByRepo  map[string][]EndpointEntry
	endpointLoadFailures   map[string]error
	endpointFanoutQueue    []RepoEntry
	endpointFanoutInFlight int
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
		},
		repoList: RepoRouteState{
			Selected: -1,
			List:     newRepoList(),
		},
		explorer: RepoExplorerRouteState{
			Selected: -1,
			List:     newEndpointList(),
			Detail: DetailState{
				ActiveTab: DetailTabEndpoints,
				Viewport:  newDetailViewport(defaultListWidth, defaultListHeight),
			},
			OperationCache: make(map[EndpointIdentity]OperationDetail),
			SpecCache:      make(map[SpecIdentity]SpecDetail),
		},
		endpointCatalogByRepo: make(map[string][]EndpointEntry),
		endpointLoadFailures:  make(map[string]error),
	}

	model.seedSelectionFromInitialRoute(route)
	model.refreshHomeEntryPresentation()
	model.refreshHomeList()
	model.refreshRepoList()
	model.refreshExplorerList()
	model.resizeLists()
	model.refreshExplorerDetailViewport()
	return model
}

func (model *rootModel) seedSelectionFromInitialRoute(route InitialRoute) {
	switch route.Kind {
	case RouteRepos:
		model.selectedNamespace = route.Namespace
		model.home.Selected = homeItemRepos
		model.explorer.Namespace = route.Namespace
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
	case homeItemEndpoints:
		if model.initialRoute.Kind != RouteRepoExplorer {
			if cmd := model.ensureRepoCatalogLoadCmd(); cmd != nil {
				cmds = append(cmds, cmd)
			}
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
		model.width = typed.Width
		model.height = typed.Height
		model.resizeLists()
		model.refreshExplorerDetailViewportIfVisible()
	case repoCatalogLoadedMsg:
		if !model.accepts(loadDomainRepoCatalog, typed.Token) {
			return model, nil
		}
		model.finishLoad(loadDomainRepoCatalog, typed.Token, nil)
		model.repos = append([]RepoEntry(nil), typed.Rows...)
		model.refreshRepoList()
		model.refreshExplorerList()
		model.refreshHomeEntryPresentation()
		model.refreshHomeList()
		if model.home.Selected == homeItemEndpoints {
			return model, model.ensureEndpointCatalogLoadCmd()
		}
		return model, nil
	case namespaceCatalogLoadedMsg:
		if !model.accepts(loadDomainNamespaces, typed.Token) {
			return model, nil
		}
		model.finishLoad(loadDomainNamespaces, typed.Token, nil)
		model.namespaces.Entries = append([]NamespaceEntry(nil), typed.Rows...)
		model.refreshNamespaceList()
		return model, nil
	case namespaceCountLoadedMsg:
		if !model.accepts(loadDomainNamespaceCount, typed.Token) {
			return model, nil
		}
		model.finishLoad(loadDomainNamespaceCount, typed.Token, nil)
		model.home.Entries = withHomeNamespaceCount(model.home.Entries, typed.Count)
		model.refreshHomeEntryPresentation()
		model.refreshHomeList()
		return model, nil
	case repoOperationCatalogLoadedMsg:
		if !model.accepts(loadDomainOperationList, typed.Token) {
			return model, nil
		}
		if model.endpointFanoutInFlight > 0 {
			model.endpointFanoutInFlight--
		}
		key := repoPath(typed.Namespace, typed.Repo)
		if typed.Err != nil {
			model.endpointLoadFailures[key] = typed.Err
		} else {
			model.endpointCatalogByRepo[key] = append([]EndpointEntry(nil), typed.Entries...)
			delete(model.endpointLoadFailures, key)
		}
		model.refreshExplorerList()
		model.refreshExplorerDetailViewport()
		if model.endpointFanoutInFlight == 0 && len(model.endpointFanoutQueue) == 0 {
			model.finishLoad(loadDomainOperationList, typed.Token, model.joinEndpointLoadErrors())
			return model, nil
		}
		return model, model.dispatchEndpointFanoutCmds(typed.Token)
	case operationListLoadedMsg:
		if !model.accepts(loadDomainOperationList, typed.Token) {
			return model, nil
		}
		model.finishLoad(loadDomainOperationList, typed.Token, nil)
		if model.selectedNamespace != "" && model.selectedRepo != "" {
			key := repoPath(model.selectedNamespace, model.selectedRepo)
			model.endpointCatalogByRepo[key] = append([]EndpointEntry(nil), typed.Entries...)
		}
		model.refreshExplorerList()
		model.refreshExplorerDetailViewport()
		return model, model.loadExplorerDetailForSelection()
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
		return model, model.loadSelectedSpecDetailIfNeeded()
	case specDetailLoadedMsg:
		if !model.accepts(loadDomainSpecDetail, typed.Token) {
			return model, nil
		}
		model.finishLoad(loadDomainSpecDetail, typed.Token, nil)
		selected, ok := model.explorer.SelectedEndpoint()
		if !ok {
			return model, nil
		}
		expected := selectedSpecIdentity(selected.Identity)
		received := selectedSpecIdentity(EndpointIdentity{
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
	case loadFailedMsg:
		if !model.accepts(typed.Domain, typed.Token) {
			return model, nil
		}
		model.finishLoad(typed.Domain, typed.Token, typed.Err)
		switch typed.Domain {
		case loadDomainRepoCatalog:
			model.refreshRepoList()
		case loadDomainNamespaces:
			model.refreshNamespaceList()
		case loadDomainNamespaceCount:
			model.home.Entries = withHomeNamespaceCountUnavailable(model.home.Entries)
			model.refreshHomeEntryPresentation()
			model.refreshHomeList()
		case loadDomainOperationDetail, loadDomainSpecDetail:
			model.refreshExplorerDetailViewport()
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
		model.namespaces.List, cmd = model.namespaces.List.Update(msg)
		model.syncNamespaceSelection()
		return model, cmd
	case RouteRepos:
		var cmd tea.Cmd
		model.repoList.List, cmd = model.repoList.List.Update(msg)
		model.syncRepoSelection()
		return model, cmd
	case RouteRepoExplorer:
		var cmd tea.Cmd
		model.explorer.List, cmd = model.explorer.List.Update(msg)
		return model, cmd
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
	case RouteRepoExplorer:
		return model.updateExplorerKey(msg)
	default:
		return model, nil
	}
}

func (model *rootModel) updateHomeKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		model.activeRoute = model.contextRouteFromHomeSelection()
		return model, model.ensureLoadForActiveSection()
	case "backspace":
		model.clearActiveSelection()
		return model, model.ensureLoadForActiveSection()
	default:
		var cmd tea.Cmd
		model.home.List, cmd = model.home.List.Update(msg)
		model.syncHomeSelection()
		model.refreshHomeEntryPresentation()
		model.refreshHomeList()
		return model, model.batchWithActiveSectionEnsure(cmd)
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
		model.activeRoute = RouteHome
		model.setHomeSelection(homeItemRepos)
		return model, model.ensureLoadForActiveSection()
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
		model.activeRoute = RouteHome
		model.syncHomeSelection()
		return model, nil
	case "enter":
		if model.repoList.Selected < 0 || model.repoList.Selected >= len(model.repoList.Entries) {
			return model, nil
		}
		selected := model.repoList.Entries[model.repoList.Selected]
		model.setRepoSelection(selected.Namespace, selected.Repo)
		model.activeRoute = RouteHome
		model.setHomeSelection(homeItemEndpoints)
		return model, model.ensureLoadForActiveSection()
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
	model.repoList.Namespace = model.selectedNamespace
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

func (model *rootModel) refreshExplorerList() {
	entries := model.filteredEndpointEntries()
	if len(entries) == 0 && len(model.endpointCatalogByRepo) == 0 && len(model.explorer.Endpoints) > 0 {
		entries = sortedEndpointEntries(model.explorer.Endpoints)
	}
	model.explorer.Endpoints = entries
	model.explorer.List.Title = "Endpoints"
	namespace := model.selectedNamespace
	repo := model.selectedRepo
	if namespace == "" {
		namespace = model.explorer.Namespace
	}
	if repo == "" {
		repo = model.explorer.Repo
	}
	if namespace != "" && repo != "" {
		model.explorer.List.Title = "Endpoints: " + namespace + "/" + repo
	} else if namespace != "" {
		model.explorer.List.Title = "Endpoints: " + namespace + "/*"
	}
	model.explorer.List.SetItems(endpointItems(entries))
	if len(entries) == 0 {
		model.explorer.Selected = -1
		model.explorer.List.ResetSelected()
		return
	}
	if model.selectedEndpoint != nil {
		for index, entry := range entries {
			if entry.Identity == *model.selectedEndpoint {
				model.explorer.Selected = index
				model.explorer.List.Select(index)
				return
			}
		}
	}
	if model.explorer.Selected < 0 || model.explorer.Selected >= len(entries) {
		model.explorer.Selected = 0
	}
	model.explorer.List.Select(model.explorer.Selected)
}

func (model *rootModel) filteredEndpointEntries() []EndpointEntry {
	entries := make([]EndpointEntry, 0)
	for key, repoEntries := range model.endpointCatalogByRepo {
		namespace, repo := splitRepoPath(key)
		if model.selectedRepo != "" {
			if namespace != model.selectedNamespace || repo != model.selectedRepo {
				continue
			}
		} else if model.selectedNamespace != "" {
			if namespace != model.selectedNamespace {
				continue
			}
		}
		entries = append(entries, repoEntries...)
	}
	return sortedEndpointEntries(entries)
}

func (model *rootModel) syncNamespaceSelection() {
	index := model.namespaces.List.GlobalIndex()
	if len(model.namespaces.Entries) == 0 || index < 0 || index >= len(model.namespaces.Entries) {
		model.namespaces.Selected = -1
		return
	}
	model.namespaces.Selected = index
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

func (model *rootModel) resizeLists() {
	leftWidth, centerWidth, rightWidth, stacked := browserPaneLayout(model.width, model.home.Selected == homeItemEndpoints)
	_, height := listSize(model.width, model.height)
	if stacked {
		model.home.List.SetSize(leftWidth, height)
		model.namespaces.List.SetSize(leftWidth, height)
		model.repoList.List.SetSize(leftWidth, height)
		model.explorer.List.SetSize(leftWidth, height)
		model.help.SetWidth(leftWidth)
		model.explorer.Detail.Viewport.SetWidth(leftWidth)
		model.explorer.Detail.Viewport.SetHeight(height)
		return
	}
	model.home.List.SetSize(leftWidth, height)
	model.namespaces.List.SetSize(centerWidth, height)
	model.repoList.List.SetSize(centerWidth, height)
	model.explorer.List.SetSize(centerWidth, height)
	model.help.SetWidth(leftWidth + centerWidth + rightWidth + 4)
	model.explorer.Detail.Viewport.SetWidth(rightWidth)
	model.explorer.Detail.Viewport.SetHeight(height)
}

func (model *rootModel) refreshExplorerDetailViewportIfVisible() {
	if model.home.Selected == homeItemEndpoints {
		model.refreshExplorerDetailViewport()
	}
}

func (model *rootModel) View() tea.View {
	s := model.viewBrowser()
	v := tea.NewView(s)
	v.AltScreen = true
	return v
}

func (model *rootModel) viewBrowser() string {
	shivaPane := model.home.List.View()
	contextPane := model.activeContextPaneView()
	footer := model.routeHelpView()

	showDetails := model.home.Selected == homeItemEndpoints
	leftWidth, _, _, stacked := browserPaneLayout(model.width, showDetails)
	if stacked {
		sections := []string{shivaPane, "", contextPane}
		if showDetails {
			sections = append(sections, "", model.endpointDetailsPaneView())
		}
		return model.layoutScreen(strings.Join(sections, "\n"), footer)
	}

	panes := []string{shivaPane, contextPane}
	if showDetails {
		panes = append(panes, model.endpointDetailsPaneView())
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, joinPanes(panes)...)
	if leftWidth <= 0 {
		body = strings.Join(panes, "\n\n")
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
	if len(model.endpointLoadFailures) > 0 {
		return strings.Join([]string{
			model.explorer.List.View(),
			"",
			model.styles.ErrorBlock(
				"Some repositories failed to load endpoints.",
				fmt.Sprintf("Failed repos: %d", len(model.endpointLoadFailures)),
			),
		}, "\n")
	}
	return model.explorer.List.View()
}

func (model *rootModel) endpointDetailsPaneView() string {
	body := strings.Join([]string{
		model.explorerTabRow(),
		"",
		model.explorer.Detail.Viewport.View(),
	}, "\n")
	return model.styles.Pane("Details", body, model.explorer.Detail.Viewport.Width())
}

func browserPaneLayout(width int, includeDetails bool) (int, int, int, bool) {
	width, _ = listSize(width, defaultListHeight)
	if !includeDetails {
		if width < 72 {
			return width, width, 0, true
		}
		left := width / 3
		if left < 24 {
			left = 24
		}
		center := width - left - 2
		if center < 30 {
			return width, width, 0, true
		}
		return left, center, 0, false
	}
	if width < 108 {
		return width, width, width, true
	}
	left := width / 4
	center := width / 3
	right := width - left - center - 4
	if left < 22 || center < 28 || right < 28 {
		return width, width, width, true
	}
	return left, center, right, false
}

func (model *rootModel) layoutScreen(body string, footer string) string {
	if model.height <= 0 {
		return strings.Join([]string{body, "", footer}, "\n")
	}

	bodyHeight := lipgloss.Height(body)
	footerHeight := lipgloss.Height(footer)
	separatorNewlines := model.height - bodyHeight - footerHeight + 1
	if separatorNewlines < 1 {
		separatorNewlines = 1
	}

	return body + strings.Repeat("\n", separatorNewlines) + footer
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
}

func (model *rootModel) contextRouteFromHomeSelection() RouteKind {
	switch model.home.Selected {
	case homeItemNamespaces:
		return RouteNamespaces
	case homeItemRepos:
		return RouteRepos
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
		model.selectedEndpoint = nil
	case homeItemRepos:
		model.selectedRepo = ""
		model.selectedEndpoint = nil
	case homeItemEndpoints:
		model.selectedEndpoint = nil
	}
	model.refreshHomeEntryPresentation()
	model.refreshHomeList()
	model.refreshRepoList()
	model.refreshExplorerList()
	model.refreshExplorerDetailViewport()
}

func (model *rootModel) setNamespaceSelection(namespace string) {
	if model.selectedNamespace != namespace {
		model.selectedRepo = ""
		model.selectedEndpoint = nil
	}
	model.selectedNamespace = namespace
	model.explorer.Namespace = namespace
	model.explorer.Repo = ""
	model.refreshHomeEntryPresentation()
	model.refreshHomeList()
	model.refreshRepoList()
	model.refreshExplorerList()
	model.refreshExplorerDetailViewport()
}

func (model *rootModel) setRepoSelection(namespace string, repo string) {
	namespaceChanged := model.selectedNamespace != namespace
	repoChanged := model.selectedRepo != repo
	model.selectedNamespace = namespace
	model.selectedRepo = repo
	model.explorer.Namespace = namespace
	model.explorer.Repo = repo
	if namespaceChanged || repoChanged {
		model.selectedEndpoint = nil
	}
	model.refreshHomeEntryPresentation()
	model.refreshHomeList()
	model.refreshRepoList()
	model.refreshExplorerList()
	model.refreshExplorerDetailViewport()
}

func (model *rootModel) setEndpointSelection(identity EndpointIdentity) {
	id := identity
	model.selectedEndpoint = &id
	model.selectedNamespace = identity.Namespace
	model.selectedRepo = identity.Repo
	model.explorer.Namespace = identity.Namespace
	model.explorer.Repo = identity.Repo
	model.refreshHomeEntryPresentation()
	model.refreshHomeList()
	model.refreshRepoList()
	model.refreshExplorerList()
	model.refreshExplorerDetailViewport()
}

func (model *rootModel) refreshHomeEntryPresentation() {
	if len(model.home.Entries) < 3 {
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

	if model.selectedEndpoint != nil {
		model.home.Entries[homeItemEndpoints].Title = endpointSelectionTitle(*model.selectedEndpoint)
	} else {
		model.home.Entries[homeItemEndpoints].Title = "Endpoints"
	}
	if model.selectedRepo != "" {
		model.home.Entries[homeItemEndpoints].Description = model.selectedRepo
	} else if model.selectedNamespace != "" {
		model.home.Entries[homeItemEndpoints].Description = model.selectedNamespace
	} else {
		model.home.Entries[homeItemEndpoints].Description = "Coming soon"
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

func splitRepoPath(path string) (string, string) {
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}

func (model *rootModel) joinEndpointLoadErrors() error {
	if len(model.endpointLoadFailures) == 0 {
		return nil
	}
	return fmt.Errorf("failed to load endpoints for %d repos", len(model.endpointLoadFailures))
}

func (model *rootModel) beginRepoCatalogLoad() RequestToken {
	return model.beginLoad(loadDomainRepoCatalog)
}

func (model *rootModel) beginNamespaceCountLoad() RequestToken {
	return model.beginLoad(loadDomainNamespaceCount)
}

func (model *rootModel) beginNamespaceCatalogLoad() RequestToken {
	return model.beginLoad(loadDomainNamespaces)
}

func (model *rootModel) ensureNamespaceCatalogLoadCmd() tea.Cmd {
	if model.async.Namespaces.Loading || model.async.Namespaces.ActiveToken > 0 {
		return nil
	}
	token := model.beginNamespaceCatalogLoad()
	return loadNamespaceCatalogCmd(context.Background(), model.service, model.options, token)
}

func (model *rootModel) ensureRepoCatalogLoadCmd() tea.Cmd {
	if model.async.RepoCatalog.Loading || model.async.RepoCatalog.ActiveToken > 0 {
		return nil
	}
	token := model.beginRepoCatalogLoad()
	return loadRepoCatalogCmd(context.Background(), model.service, model.options, token)
}

func (model *rootModel) ensureEndpointCatalogLoadCmd() tea.Cmd {
	repos := model.endpointScopeRepos()
	if len(repos) == 0 {
		return nil
	}
	missing := make([]RepoEntry, 0, len(repos))
	for _, repo := range repos {
		key := repoPath(repo.Namespace, repo.Repo)
		if _, ok := model.endpointCatalogByRepo[key]; ok {
			continue
		}
		if repo.Row.Namespace != "" && repo.Row.SnapshotRevision == nil && repo.Row.ActiveAPICount == 0 {
			continue
		}
		missing = append(missing, repo)
	}
	if len(missing) == 0 {
		return nil
	}

	token := model.beginOperationListLoad()
	model.endpointFanoutQueue = append([]RepoEntry(nil), missing...)
	model.endpointFanoutInFlight = 0
	model.endpointLoadFailures = make(map[string]error)
	return model.dispatchEndpointFanoutCmds(token)
}

func (model *rootModel) endpointScopeRepos() []RepoEntry {
	if model.selectedNamespace != "" && model.selectedRepo != "" {
		for _, repo := range model.repos {
			if repo.Namespace == model.selectedNamespace && repo.Repo == model.selectedRepo {
				return []RepoEntry{repo}
			}
		}
		return []RepoEntry{{Namespace: model.selectedNamespace, Repo: model.selectedRepo}}
	}
	if model.selectedNamespace != "" {
		return repoEntriesByNamespace(model.repos, model.selectedNamespace)
	}
	return append([]RepoEntry(nil), model.repos...)
}

func (model *rootModel) dispatchEndpointFanoutCmds(token RequestToken) tea.Cmd {
	cmds := make([]tea.Cmd, 0, endpointFanoutConcurrency)
	for model.endpointFanoutInFlight < endpointFanoutConcurrency && len(model.endpointFanoutQueue) > 0 {
		next := model.endpointFanoutQueue[0]
		model.endpointFanoutQueue = model.endpointFanoutQueue[1:]
		model.endpointFanoutInFlight++
		cmds = append(cmds, loadRepoOperationCatalogCmd(
			context.Background(),
			model.service,
			next.Namespace,
			next.Repo,
			model.options,
			token,
		))
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

func (model *rootModel) ensureLoadForActiveSection() tea.Cmd {
	switch model.home.Selected {
	case homeItemNamespaces:
		return model.ensureNamespaceCatalogLoadCmd()
	case homeItemRepos:
		return batchCmds(model.ensureNamespaceCatalogLoadCmd(), model.ensureRepoCatalogLoadCmd())
	case homeItemEndpoints:
		return batchCmds(model.ensureRepoCatalogLoadCmd(), model.ensureEndpointCatalogLoadCmd())
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
	case loadDomainNamespaces:
		return &model.async.Namespaces
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
