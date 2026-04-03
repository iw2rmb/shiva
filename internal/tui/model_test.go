package tui

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	clioutput "github.com/iw2rmb/shiva/internal/cli/output"
	"github.com/iw2rmb/shiva/internal/cli/request"
)

func TestNewRootModelSeedsRouteSpecificState(t *testing.T) {
	t.Parallel()

	model := newRootModel(&fakeBrowserService{}, InitialRoute{
		Kind:      RouteRepoExplorer,
		Namespace: "acme",
		Repo:      "platform",
	}, RequestOptions{
		Profile: "work",
		Offline: true,
	})

	if model.activeRoute != RouteRepoExplorer {
		t.Fatalf("expected active route %q, got %q", RouteRepoExplorer, model.activeRoute)
	}
	if model.repoList.Namespace != "acme" {
		t.Fatalf("expected repo route namespace to seed from initial route, got %q", model.repoList.Namespace)
	}
	if model.explorer.Namespace != "acme" || model.explorer.Repo != "platform" {
		t.Fatalf("expected explorer route to seed from initial route, got %+v", model.explorer)
	}
	if model.explorer.Detail.ActiveTab != DetailTabEndpoints {
		t.Fatalf("expected default detail tab %q, got %q", DetailTabEndpoints, model.explorer.Detail.ActiveTab)
	}
	if model.namespaces.List.Title != "Namespaces" {
		t.Fatalf("expected namespace list to initialize, got %q", model.namespaces.List.Title)
	}
	if model.repoList.List.Title != "Repositories" {
		t.Fatalf("expected repo list to initialize, got %q", model.repoList.List.Title)
	}
}

func TestNewRootModelHomeRouteShowsShivaSections(t *testing.T) {
	t.Parallel()

	model := newRootModel(&fakeBrowserService{}, InitialRoute{Kind: RouteHome}, RequestOptions{})

	if model.activeRoute != RouteHome {
		t.Fatalf("expected active route %q, got %q", RouteHome, model.activeRoute)
	}
	if model.home.List.Title != "SHIVA" {
		t.Fatalf("expected home list title SHIVA, got %q", model.home.List.Title)
	}
	if len(model.home.Entries) != 2 {
		t.Fatalf("expected two home entries, got %d", len(model.home.Entries))
	}
	if model.home.Entries[0].Title != "Repos" || model.home.Entries[1].Title != "Endpoints" {
		t.Fatalf("unexpected home entries %+v", model.home.Entries)
	}
}

func TestRootModelEnterReposFromHomeOpensNamespaces(t *testing.T) {
	t.Parallel()

	model := newRootModel(&fakeBrowserService{}, InitialRoute{Kind: RouteHome}, RequestOptions{})

	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(*rootModel)

	if model.activeRoute != RouteNamespaces {
		t.Fatalf("expected route %q after selecting Repos, got %q", RouteNamespaces, model.activeRoute)
	}
}

func TestRootModelNamespacesListUsesDefaultFiltering(t *testing.T) {
	t.Parallel()

	model := newRootModel(&fakeBrowserService{}, InitialRoute{Kind: RouteNamespaces}, RequestOptions{})

	if !model.namespaces.List.FilteringEnabled() {
		t.Fatalf("expected namespace list filtering to be enabled")
	}
	if !model.namespaces.List.ShowFilter() {
		t.Fatalf("expected namespace list filter input to be visible")
	}
}

func TestRootModelNamespacesFilterInputReducesVisibleItems(t *testing.T) {
	t.Parallel()

	model := newRootModel(&fakeBrowserService{}, InitialRoute{Kind: RouteNamespaces}, RequestOptions{})
	token := model.beginNamespaceCatalogLoad()

	updated, _ := model.Update(namespaceCatalogLoadedMsg{
		Token: token,
		Rows: []NamespaceEntry{
			{Namespace: "acme", RepoCount: 1},
			{Namespace: "beta", RepoCount: 1},
		},
	})
	model = updated.(*rootModel)
	model.namespaces.List.SetFilterState(list.Filtering)

	updated, cmd := model.Update(tea.KeyPressMsg{Code: 'b', Text: "b"})
	model = updated.(*rootModel)
	if cmd == nil {
		t.Fatalf("expected filter update command")
	}
	model = applyListCmd(model, cmd)

	visible := model.namespaces.List.VisibleItems()
	if len(visible) != 1 {
		t.Fatalf("expected one visible namespace after filter, got %d", len(visible))
	}
	item, ok := visible[0].(namespaceListItem)
	if !ok {
		t.Fatalf("expected namespace list item, got %T", visible[0])
	}
	if item.title != "beta" {
		t.Fatalf("expected filtered namespace beta, got %q", item.title)
	}
}

func TestRootModelTypingQInNamespaceFilterDoesNotQuit(t *testing.T) {
	t.Parallel()

	model := newRootModel(&fakeBrowserService{}, InitialRoute{Kind: RouteNamespaces}, RequestOptions{})
	model.namespaces.List.SetFilterState(list.Filtering)

	updated, cmd := model.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	model = updated.(*rootModel)

	if model.activeRoute != RouteNamespaces {
		t.Fatalf("expected to stay on namespaces route, got %q", model.activeRoute)
	}
	if model.namespaces.List.FilterValue() != "q" {
		t.Fatalf("expected filter input to contain q, got %q", model.namespaces.List.FilterValue())
	}
	if cmd != nil {
		if _, ok := cmd().(tea.QuitMsg); ok {
			t.Fatalf("expected q in filter input to not quit the app")
		}
	}
}

func TestRootModelEnterFilteredNamespaceOpensSelectedNamespaceRepos(t *testing.T) {
	t.Parallel()

	model := newRootModel(&fakeBrowserService{}, InitialRoute{Kind: RouteNamespaces}, RequestOptions{})
	namespaceToken := model.beginNamespaceCatalogLoad()
	repoToken := model.beginRepoCatalogLoad()

	updated, _ := model.Update(namespaceCatalogLoadedMsg{
		Token: namespaceToken,
		Rows: []NamespaceEntry{
			{Namespace: "acme", RepoCount: 1},
			{Namespace: "beta", RepoCount: 1},
		},
	})
	model = updated.(*rootModel)
	updated, _ = model.Update(repoCatalogLoadedMsg{
		Token: repoToken,
		Rows: []RepoEntry{
			{Namespace: "acme", Repo: "gateway"},
			{Namespace: "beta", Repo: "payments"},
		},
	})
	model = updated.(*rootModel)

	model.namespaces.List.SetFilterText("be")
	model.syncNamespaceSelection()

	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(*rootModel)

	if model.activeRoute != RouteRepos {
		t.Fatalf("expected route %q, got %q", RouteRepos, model.activeRoute)
	}
	if model.repoList.Namespace != "beta" {
		t.Fatalf("expected filtered namespace beta, got %q", model.repoList.Namespace)
	}
	if len(model.repoList.Entries) != 1 || model.repoList.Entries[0].Repo != "payments" {
		t.Fatalf("expected beta/payments repo entries, got %+v", model.repoList.Entries)
	}
}

func applyListCmd(model *rootModel, cmd tea.Cmd) *rootModel {
	if cmd == nil {
		return model
	}

	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		updated, _ := model.Update(msg)
		return updated.(*rootModel)
	}

	for _, command := range batch {
		if command == nil {
			continue
		}
		updated, _ := model.Update(command())
		model = updated.(*rootModel)
	}
	return model
}

func collectCmdMessages(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}

	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{msg}
	}

	collected := make([]tea.Msg, 0, len(batch))
	for _, command := range batch {
		collected = append(collected, collectCmdMessages(command)...)
	}
	return collected
}

func TestRootModelInitStartsRepoCatalogLoad(t *testing.T) {
	t.Parallel()

	service := &fakeBrowserService{
		listNamespacesBody: []byte(`[{"namespace":"acme","repo_count":1,"all_pending":false}]`),
	}

	model := newRootModel(service, InitialRoute{Kind: RouteNamespaces}, RequestOptions{Offline: true})
	cmd := model.Init()
	if cmd == nil {
		t.Fatalf("expected init load command")
	}
	if !model.async.Namespaces.Loading {
		t.Fatalf("expected init to mark namespace catalog loading")
	}

	var loaded namespaceCatalogLoadedMsg
	var namespaceLoaded bool
	for _, msg := range collectCmdMessages(cmd) {
		typed, ok := msg.(namespaceCatalogLoadedMsg)
		if !ok {
			continue
		}
		loaded = typed
		namespaceLoaded = true
		break
	}
	if !namespaceLoaded {
		t.Fatalf("expected namespaceCatalogLoadedMsg in init batch")
	}
	if loaded.Token != model.async.Namespaces.ActiveToken {
		t.Fatalf("expected token %d, got %d", model.async.Namespaces.ActiveToken, loaded.Token)
	}
}

func TestRootModelInitRouteReposStartsNamespaceAndRepoLoads(t *testing.T) {
	t.Parallel()

	service := &fakeBrowserService{
		listNamespacesBody: []byte(`[{"namespace":"acme","repo_count":1,"all_pending":false}]`),
		listReposBody:      []byte(`[{"namespace":"acme","repo":"platform"}]`),
	}

	model := newRootModel(service, InitialRoute{Kind: RouteRepos, Namespace: "acme"}, RequestOptions{Offline: true})
	cmd := model.Init()
	if cmd == nil {
		t.Fatalf("expected init load command")
	}
	if !model.async.Namespaces.Loading {
		t.Fatalf("expected namespace catalog loading on init")
	}
	if !model.async.RepoCatalog.Loading {
		t.Fatalf("expected repo catalog loading on init")
	}

	var namespaceLoaded bool
	var repoLoaded bool
	for _, msg := range collectCmdMessages(cmd) {
		switch msg.(type) {
		case namespaceCatalogLoadedMsg:
			namespaceLoaded = true
		case repoCatalogLoadedMsg:
			repoLoaded = true
		}
	}
	if !namespaceLoaded {
		t.Fatalf("expected namespaceCatalogLoadedMsg from init batch")
	}
	if !repoLoaded {
		t.Fatalf("expected repoCatalogLoadedMsg from init batch")
	}
}

func TestRootModelInitRepoExplorerSkipsRepoCatalogLoad(t *testing.T) {
	t.Parallel()

	service := &fakeBrowserService{
		listReposBody:      []byte(`[{"namespace":"acme","repo":"platform"}]`),
		listOperationsBody: []byte(`[]`),
	}

	model := newRootModel(service, InitialRoute{
		Kind:      RouteRepoExplorer,
		Namespace: "acme",
		Repo:      "platform",
	}, RequestOptions{})
	cmd := model.Init()
	if cmd == nil {
		t.Fatalf("expected init command")
	}
	if model.async.RepoCatalog.Loading {
		t.Fatalf("expected repo catalog load to be skipped for direct repo route")
	}
	if !model.async.OperationList.Loading {
		t.Fatalf("expected operation list load to start for direct repo route")
	}

	var operationLoaded bool
	for _, msg := range collectCmdMessages(cmd) {
		if _, ok := msg.(operationListLoadedMsg); ok {
			operationLoaded = true
			break
		}
	}
	if !operationLoaded {
		t.Fatalf("expected operationListLoadedMsg in init batch")
	}
	if service.listReposCall != 0 {
		t.Fatalf("expected no repo list calls on direct repo route init, got %d", service.listReposCall)
	}
}

func TestLoadRepoCatalogCmdCarriesRequestToken(t *testing.T) {
	t.Parallel()

	service := &fakeBrowserService{
		listReposBody: []byte(`[{"namespace":"acme","repo":"platform"}]`),
	}

	msg := loadRepoCatalogCmd(context.Background(), service, RequestOptions{Offline: true}, 7)()
	loaded, ok := msg.(repoCatalogLoadedMsg)
	if !ok {
		t.Fatalf("expected repoCatalogLoadedMsg, got %T", msg)
	}
	if loaded.Token != 7 {
		t.Fatalf("expected token 7, got %d", loaded.Token)
	}
	if !reflect.DeepEqual(loaded.Rows, []RepoEntry{{
		Namespace: "acme",
		Repo:      "platform",
		Row:       clioutput.RepoRow{Namespace: "acme", Repo: "platform"},
	}}) {
		t.Fatalf("unexpected repo entries %+v", loaded.Rows)
	}
}

func TestRootModelIgnoresStaleRepoCatalogMessages(t *testing.T) {
	t.Parallel()

	model := newRootModel(&fakeBrowserService{}, InitialRoute{Kind: RouteNamespaces}, RequestOptions{})

	staleToken := model.beginRepoCatalogLoad()
	currentToken := model.beginRepoCatalogLoad()

	updated, _ := model.Update(repoCatalogLoadedMsg{
		Token: staleToken,
		Rows: []RepoEntry{{
			Namespace: "stale",
			Repo:      "old",
		}},
	})
	model = updated.(*rootModel)

	if len(model.repos) != 0 {
		t.Fatalf("expected stale repo catalog to be ignored, got %+v", model.repos)
	}
	if model.async.RepoCatalog.ActiveToken != currentToken || !model.async.RepoCatalog.Loading {
		t.Fatalf("expected newer repo catalog request to remain active, got %+v", model.async.RepoCatalog)
	}

	updated, _ = model.Update(repoCatalogLoadedMsg{
		Token: currentToken,
		Rows: []RepoEntry{{
			Namespace: "acme",
			Repo:      "platform",
		}},
	})
	model = updated.(*rootModel)

	if want := []RepoEntry{{Namespace: "acme", Repo: "platform"}}; !reflect.DeepEqual(model.repos, want) {
		t.Fatalf("expected latest repo catalog to apply, got %+v", model.repos)
	}
	if model.async.RepoCatalog.Loading {
		t.Fatalf("expected repo catalog load to finish")
	}
}

func TestRootModelIgnoresStaleOperationListMessages(t *testing.T) {
	t.Parallel()

	model := newRootModel(&fakeBrowserService{}, InitialRoute{
		Kind:      RouteRepoExplorer,
		Namespace: "acme",
		Repo:      "platform",
	}, RequestOptions{})

	staleToken := model.beginOperationListLoad()
	currentToken := model.beginOperationListLoad()

	updated, _ := model.Update(operationListLoadedMsg{
		Token: staleToken,
		Entries: []EndpointEntry{{
			Identity: EndpointIdentity{Namespace: "acme", Repo: "platform", OperationID: "staleOp"},
		}},
	})
	model = updated.(*rootModel)

	if len(model.explorer.Endpoints) != 0 {
		t.Fatalf("expected stale operation list to be ignored, got %+v", model.explorer.Endpoints)
	}

	updated, _ = model.Update(operationListLoadedMsg{
		Token: currentToken,
		Entries: []EndpointEntry{{
			Identity: EndpointIdentity{Namespace: "acme", Repo: "platform", OperationID: "newOp"},
		}},
	})
	model = updated.(*rootModel)

	if len(model.explorer.Endpoints) != 1 || model.explorer.Endpoints[0].Identity.OperationID != "newOp" {
		t.Fatalf("expected latest operation list to apply, got %+v", model.explorer.Endpoints)
	}
}

func TestRootModelBuildsNamespaceEntriesFromCatalog(t *testing.T) {
	t.Parallel()

	model := newRootModel(&fakeBrowserService{}, InitialRoute{Kind: RouteNamespaces}, RequestOptions{})
	token := model.beginNamespaceCatalogLoad()

	updated, _ := model.Update(namespaceCatalogLoadedMsg{
		Token: token,
		Rows: []NamespaceEntry{
			{Namespace: "acme", RepoCount: 2},
			{Namespace: "beta", RepoCount: 1},
		},
	})
	model = updated.(*rootModel)

	if model.namespaces.Selected != 0 {
		t.Fatalf("expected first namespace selected, got %d", model.namespaces.Selected)
	}
	want := []NamespaceEntry{
		{Namespace: "acme", RepoCount: 2},
		{Namespace: "beta", RepoCount: 1},
	}
	if !reflect.DeepEqual(model.namespaces.Entries, want) {
		t.Fatalf("unexpected namespaces %+v", model.namespaces.Entries)
	}
}

func TestRootModelEnterOpensSelectedNamespaceRepos(t *testing.T) {
	t.Parallel()

	model := newRootModel(&fakeBrowserService{}, InitialRoute{Kind: RouteNamespaces}, RequestOptions{})
	namespaceToken := model.beginNamespaceCatalogLoad()
	repoToken := model.beginRepoCatalogLoad()

	updated, _ := model.Update(namespaceCatalogLoadedMsg{
		Token: namespaceToken,
		Rows: []NamespaceEntry{
			{Namespace: "acme", RepoCount: 2},
			{Namespace: "beta", RepoCount: 1},
		},
	})
	model = updated.(*rootModel)
	updated, _ = model.Update(repoCatalogLoadedMsg{
		Token: repoToken,
		Rows: []RepoEntry{
			{Namespace: "acme", Repo: "gateway"},
			{Namespace: "acme", Repo: "platform"},
			{Namespace: "beta", Repo: "payments"},
		},
	})
	model = updated.(*rootModel)

	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	model = updated.(*rootModel)
	if model.namespaces.Selected != 1 {
		t.Fatalf("expected selection to move to beta, got %d", model.namespaces.Selected)
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(*rootModel)

	if model.activeRoute != RouteRepos {
		t.Fatalf("expected route %q, got %q", RouteRepos, model.activeRoute)
	}
	if model.repoList.Namespace != "beta" {
		t.Fatalf("expected beta namespace, got %q", model.repoList.Namespace)
	}
	want := []RepoEntry{{Namespace: "beta", Repo: "payments"}}
	if !reflect.DeepEqual(model.repoList.Entries, want) {
		t.Fatalf("unexpected repo entries %+v", model.repoList.Entries)
	}
	if model.repoList.Selected != 0 {
		t.Fatalf("expected first repo selected, got %d", model.repoList.Selected)
	}
}

func TestRootModelRepoCatalogLoadAfterNamespaceEnterDoesNotResetToHome(t *testing.T) {
	t.Parallel()

	model := newRootModel(&fakeBrowserService{}, InitialRoute{Kind: RouteHome}, RequestOptions{})
	namespaceToken := model.beginNamespaceCatalogLoad()
	repoToken := model.beginRepoCatalogLoad()

	updated, _ := model.Update(namespaceCatalogLoadedMsg{
		Token: namespaceToken,
		Rows: []NamespaceEntry{
			{Namespace: "acme", RepoCount: 1},
		},
	})
	model = updated.(*rootModel)
	updated, _ = model.Update(repoCatalogLoadedMsg{
		Token: repoToken,
		Rows: []RepoEntry{
			{Namespace: "acme", Repo: "platform"},
		},
	})
	model = updated.(*rootModel)

	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(*rootModel)
	if model.activeRoute != RouteNamespaces {
		t.Fatalf("expected route %q after opening Repos section, got %q", RouteNamespaces, model.activeRoute)
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(*rootModel)
	if model.activeRoute != RouteRepos {
		t.Fatalf("expected route %q after opening namespace, got %q", RouteRepos, model.activeRoute)
	}

	token := model.beginRepoCatalogLoad()

	updated, _ = model.Update(repoCatalogLoadedMsg{
		Token: token,
		Rows: []RepoEntry{
			{Namespace: "acme", Repo: "platform"},
		},
	})
	model = updated.(*rootModel)

	if model.activeRoute != RouteRepos {
		t.Fatalf("expected to stay on %q after repo catalog load, got %q", RouteRepos, model.activeRoute)
	}
}

func TestRootModelEscReturnsFromReposToNamespaces(t *testing.T) {
	t.Parallel()

	model := newRootModel(&fakeBrowserService{}, InitialRoute{Kind: RouteRepos, Namespace: "acme"}, RequestOptions{})
	namespaceToken := model.beginNamespaceCatalogLoad()
	updated, _ := model.Update(namespaceCatalogLoadedMsg{
		Token: namespaceToken,
		Rows: []NamespaceEntry{
			{Namespace: "acme", RepoCount: 2},
		},
	})
	model = updated.(*rootModel)
	token := model.beginRepoCatalogLoad()

	updated, _ = model.Update(repoCatalogLoadedMsg{
		Token: token,
		Rows: []RepoEntry{
			{Namespace: "acme", Repo: "gateway"},
			{Namespace: "acme", Repo: "platform"},
		},
	})
	model = updated.(*rootModel)

	if model.activeRoute != RouteRepos {
		t.Fatalf("expected initial route to switch to repos, got %q", model.activeRoute)
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	model = updated.(*rootModel)

	if model.activeRoute != RouteNamespaces {
		t.Fatalf("expected route %q, got %q", RouteNamespaces, model.activeRoute)
	}
	if model.namespaces.Selected != 0 {
		t.Fatalf("expected namespace selection to be preserved, got %d", model.namespaces.Selected)
	}
}

func TestRootModelRendersEmptyCatalogState(t *testing.T) {
	t.Parallel()

	model := newRootModel(&fakeBrowserService{}, InitialRoute{Kind: RouteNamespaces}, RequestOptions{})
	token := model.beginNamespaceCatalogLoad()

	updated, _ := model.Update(namespaceCatalogLoadedMsg{Token: token})
	model = updated.(*rootModel)

	if got := model.View().Content; !strings.Contains(got, "No namespaces found.") {
		t.Fatalf("expected empty namespace state, got %q", got)
	}
}

func TestRootModelRendersStartupLoadFailure(t *testing.T) {
	t.Parallel()

	model := newRootModel(&fakeBrowserService{}, InitialRoute{Kind: RouteNamespaces}, RequestOptions{})
	token := model.beginNamespaceCatalogLoad()

	updated, _ := model.Update(loadFailedMsg{
		Domain: loadDomainNamespaces,
		Token:  token,
		Err:    errors.New("catalog unavailable"),
	})
	model = updated.(*rootModel)

	view := model.View().Content
	if !strings.Contains(view, "Failed to load namespaces.") {
		t.Fatalf("expected failure heading, got %q", view)
	}
	if !strings.Contains(view, "catalog unavailable") {
		t.Fatalf("expected failure cause, got %q", view)
	}
}

func TestRootModelRouteLocalHelpShowsOnlyActiveRouteBindings(t *testing.T) {
	t.Parallel()

	model := newRootModel(&fakeBrowserService{}, InitialRoute{Kind: RouteNamespaces}, RequestOptions{})

	model.activeRoute = RouteNamespaces
	namespaceHelp := model.routeHelpView()
	if !strings.Contains(namespaceHelp, "open namespace") {
		t.Fatalf("expected namespace help to include namespace action, got %q", namespaceHelp)
	}
	if strings.Contains(namespaceHelp, "open repo") || strings.Contains(namespaceHelp, "switch tab") {
		t.Fatalf("expected namespace help to exclude repo/explorer actions, got %q", namespaceHelp)
	}

	model.activeRoute = RouteRepos
	repoHelp := model.routeHelpView()
	if !strings.Contains(repoHelp, "open repo") {
		t.Fatalf("expected repos help to include repo action, got %q", repoHelp)
	}
	if strings.Contains(repoHelp, "open namespace") || strings.Contains(repoHelp, "switch tab") {
		t.Fatalf("expected repos help to exclude namespace/explorer actions, got %q", repoHelp)
	}

	model.activeRoute = RouteRepoExplorer
	explorerHelp := model.routeHelpView()
	if !strings.Contains(explorerHelp, "switch tab") || !strings.Contains(explorerHelp, "scroll details") {
		t.Fatalf("expected explorer help to include explorer actions, got %q", explorerHelp)
	}
	if strings.Contains(explorerHelp, "open namespace") || strings.Contains(explorerHelp, "open repo") {
		t.Fatalf("expected explorer help to exclude namespace/repo actions, got %q", explorerHelp)
	}
}

func TestRootModelIgnoresStaleOperationDetailMessages(t *testing.T) {
	t.Parallel()

	model := newRootModel(&fakeBrowserService{}, InitialRoute{
		Kind:      RouteRepoExplorer,
		Namespace: "acme",
		Repo:      "platform",
	}, RequestOptions{})
	model.explorer.Endpoints = []EndpointEntry{
		{Identity: EndpointIdentity{Namespace: "acme", Repo: "platform", OperationID: "oldOp"}},
		{Identity: EndpointIdentity{Namespace: "acme", Repo: "platform", OperationID: "newOp"}},
	}
	model.explorer.Selected = 1

	staleToken := model.beginOperationDetailLoad()
	currentToken := model.beginOperationDetailLoad()

	updated, _ := model.Update(operationDetailLoadedMsg{
		Token: staleToken,
		Detail: OperationDetail{
			Endpoint: EndpointIdentity{Namespace: "acme", Repo: "platform", OperationID: "oldOp"},
			Body:     json.RawMessage(`{"operationId":"oldOp"}`),
		},
	})
	model = updated.(*rootModel)

	if model.explorer.Detail.Operation != nil {
		t.Fatalf("expected stale operation detail to be ignored, got %+v", model.explorer.Detail.Operation)
	}
	selected, ok := model.explorer.SelectedEndpoint()
	if !ok || selected.Identity.OperationID != "newOp" {
		t.Fatalf("expected endpoint selection to remain on newer entry, got %+v", selected)
	}

	updated, _ = model.Update(operationDetailLoadedMsg{
		Token: currentToken,
		Detail: OperationDetail{
			Endpoint: EndpointIdentity{Namespace: "acme", Repo: "platform", OperationID: "newOp"},
			Body:     json.RawMessage(`{"operationId":"newOp"}`),
		},
	})
	model = updated.(*rootModel)

	if model.explorer.Detail.Operation == nil || model.explorer.Detail.Operation.Endpoint.OperationID != "newOp" {
		t.Fatalf("expected latest operation detail to apply, got %+v", model.explorer.Detail.Operation)
	}
	selected, ok = model.explorer.SelectedEndpoint()
	if !ok || selected.Identity.OperationID != "newOp" {
		t.Fatalf("expected endpoint selection to remain on new entry, got %+v", selected)
	}
}

func TestRootModelIgnoresStaleSpecDetailMessages(t *testing.T) {
	t.Parallel()

	model := newRootModel(&fakeBrowserService{}, InitialRoute{
		Kind:      RouteRepoExplorer,
		Namespace: "acme",
		Repo:      "platform",
	}, RequestOptions{})
	model.explorer.Endpoints = []EndpointEntry{
		{
			Identity: EndpointIdentity{
				Namespace:   "acme",
				Repo:        "platform",
				API:         "new.yaml",
				Method:      "get",
				Path:        "/pets",
				OperationID: "listPets",
			},
		},
	}
	model.explorer.Selected = 0

	staleToken := model.beginSpecDetailLoad()
	currentToken := model.beginSpecDetailLoad()

	updated, _ := model.Update(specDetailLoadedMsg{
		Token: staleToken,
		Detail: SpecDetail{
			Namespace: "acme",
			Repo:      "platform",
			API:       "old.yaml",
			Body:      json.RawMessage(`{"openapi":"3.1.0"}`),
		},
	})
	model = updated.(*rootModel)

	if model.explorer.Detail.Spec != nil {
		t.Fatalf("expected stale spec detail to be ignored, got %+v", model.explorer.Detail.Spec)
	}

	updated, _ = model.Update(specDetailLoadedMsg{
		Token: currentToken,
		Detail: SpecDetail{
			Namespace: "acme",
			Repo:      "platform",
			API:       "new.yaml",
			Body:      json.RawMessage(`{"openapi":"3.1.0"}`),
		},
	})
	model = updated.(*rootModel)

	if model.explorer.Detail.Spec == nil || model.explorer.Detail.Spec.API != "new.yaml" {
		t.Fatalf("expected latest spec detail to apply, got %+v", model.explorer.Detail.Spec)
	}
}

func TestLoadOperationDetailMsgUsesCallerContext(t *testing.T) {
	t.Parallel()

	ctx := context.WithValue(context.Background(), contextKey("request"), "token")
	service := &fakeBrowserService{
		operationBody: []byte(`{"operationId":"getPet"}`),
	}

	msg := loadOperationDetailMsg(ctx, service, request.Envelope{
		Namespace:   "acme",
		Repo:        "platform",
		OperationID: "getPet",
	}, RequestOptions{}, 11)

	loaded, ok := msg.(operationDetailLoadedMsg)
	if !ok {
		t.Fatalf("expected operationDetailLoadedMsg, got %T", msg)
	}
	if loaded.Token != 11 {
		t.Fatalf("expected token 11, got %d", loaded.Token)
	}
	if service.lastContextValue != "token" {
		t.Fatalf("expected caller context to reach service, got %q", service.lastContextValue)
	}
}

func TestLoadSpecDetailMsgCarriesFailureToken(t *testing.T) {
	t.Parallel()

	msg := loadSpecDetailMsg(context.Background(), &fakeBrowserService{
		specErr: errors.New("boom"),
	}, request.Envelope{
		Namespace: "acme",
		Repo:      "platform",
		API:       "pets.yaml",
	}, RequestOptions{}, 13)

	failed, ok := msg.(loadFailedMsg)
	if !ok {
		t.Fatalf("expected loadFailedMsg, got %T", msg)
	}
	if failed.Domain != loadDomainSpecDetail || failed.Token != 13 {
		t.Fatalf("unexpected failure message %+v", failed)
	}
}

func TestRootModelConvertsWindowSizeIntoTypedResizeMessage(t *testing.T) {
	t.Parallel()

	model := newRootModel(&fakeBrowserService{}, InitialRoute{Kind: RouteNamespaces}, RequestOptions{})

	next, cmd := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if cmd == nil {
		t.Fatalf("expected resize command")
	}
	resize, ok := cmd().(resizeMsg)
	if !ok {
		t.Fatalf("expected resizeMsg, got %T", cmd())
	}
	if resize.Width != 120 || resize.Height != 40 {
		t.Fatalf("unexpected resize message %+v", resize)
	}

	updated, _ := next.(*rootModel).Update(resize)
	model = updated.(*rootModel)
	if model.width != 120 || model.height != 40 {
		t.Fatalf("expected model size to update, got %dx%d", model.width, model.height)
	}
}

func TestLayoutScreenWithoutKnownHeightKeepsDefaultSpacing(t *testing.T) {
	t.Parallel()

	model := newRootModel(&fakeBrowserService{}, InitialRoute{Kind: RouteHome}, RequestOptions{})
	model.height = 0

	rendered := model.layoutScreen("body", "footer")
	if rendered != "body\n\nfooter" {
		t.Fatalf("unexpected layout output %q", rendered)
	}
}

func TestLayoutScreenPinsFooterToLastTerminalRow(t *testing.T) {
	t.Parallel()

	model := newRootModel(&fakeBrowserService{}, InitialRoute{Kind: RouteHome}, RequestOptions{})
	model.height = 7

	rendered := model.layoutScreen("line-1\nline-2", "footer")
	if lipgloss.Height(rendered) != 7 {
		t.Fatalf("expected layout height 7, got %d", lipgloss.Height(rendered))
	}
	lines := strings.Split(rendered, "\n")
	if lines[len(lines)-1] != "footer" {
		t.Fatalf("expected footer on final line, got %q", lines[len(lines)-1])
	}
}

type fakeBrowserService struct {
	listNamespacesBody []byte
	listNamespacesErr  error
	listNamespacesCall int
	listReposBody      []byte
	listReposErr       error
	listReposCall      int
	listOperationsBody []byte
	listOperationsErr  error
	listOperationsCall int
	lastOperationQuery request.Envelope
	getOperationCall   int
	lastOperationGet   request.Envelope
	operationBody      []byte
	operationErr       error
	getSpecCall        int
	lastSpecGet        request.Envelope
	lastSpecFormat     SpecFormat
	specBody           []byte
	specErr            error
	lastContextValue   any
}

func (service *fakeBrowserService) ListNamespaces(
	ctx context.Context,
	options RequestOptions,
	format clioutput.ListFormat,
) ([]byte, error) {
	service.lastContextValue = ctx.Value(contextKey("request"))
	service.listNamespacesCall++
	_ = options
	_ = format
	return service.listNamespacesBody, service.listNamespacesErr
}

func (service *fakeBrowserService) ListRepos(
	ctx context.Context,
	options RequestOptions,
	format clioutput.ListFormat,
) ([]byte, error) {
	service.lastContextValue = ctx.Value(contextKey("request"))
	service.listReposCall++
	_ = options
	_ = format
	return service.listReposBody, service.listReposErr
}

func (service *fakeBrowserService) ListOperations(
	ctx context.Context,
	selector request.Envelope,
	options RequestOptions,
	format clioutput.ListFormat,
) ([]byte, error) {
	service.lastContextValue = ctx.Value(contextKey("request"))
	service.listOperationsCall++
	service.lastOperationQuery = selector
	_ = options
	_ = format
	return service.listOperationsBody, service.listOperationsErr
}

func (service *fakeBrowserService) GetOperation(
	ctx context.Context,
	selector request.Envelope,
	options RequestOptions,
) ([]byte, error) {
	service.lastContextValue = ctx.Value(contextKey("request"))
	service.getOperationCall++
	service.lastOperationGet = selector
	_ = selector
	_ = options
	return service.operationBody, service.operationErr
}

func (service *fakeBrowserService) GetSpec(
	ctx context.Context,
	selector request.Envelope,
	options RequestOptions,
	format SpecFormat,
) ([]byte, error) {
	service.lastContextValue = ctx.Value(contextKey("request"))
	service.getSpecCall++
	service.lastSpecGet = selector
	service.lastSpecFormat = format
	_ = options
	return service.specBody, service.specErr
}

type contextKey string
