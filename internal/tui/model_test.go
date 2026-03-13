package tui

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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

func TestRootModelInitStartsRepoCatalogLoad(t *testing.T) {
	t.Parallel()

	service := &fakeBrowserService{
		listReposBody: []byte(`[{"namespace":"acme","repo":"platform"}]`),
	}

	model := newRootModel(service, InitialRoute{Kind: RouteNamespaces}, RequestOptions{Offline: true})
	cmd := model.Init()
	if cmd == nil {
		t.Fatalf("expected init load command")
	}
	if !model.async.RepoCatalog.Loading {
		t.Fatalf("expected init to mark repo catalog loading")
	}

	msg := cmd()
	loaded, ok := msg.(repoCatalogLoadedMsg)
	if !ok {
		t.Fatalf("expected repoCatalogLoadedMsg, got %T", msg)
	}
	if loaded.Token != model.async.RepoCatalog.ActiveToken {
		t.Fatalf("expected token %d, got %d", model.async.RepoCatalog.ActiveToken, loaded.Token)
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
	token := model.beginRepoCatalogLoad()

	updated, _ := model.Update(repoCatalogLoadedMsg{
		Token: token,
		Rows: []RepoEntry{
			{Namespace: "beta", Repo: "payments"},
			{Namespace: "acme", Repo: "gateway"},
			{Namespace: "acme", Repo: "platform"},
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
	token := model.beginRepoCatalogLoad()

	updated, _ := model.Update(repoCatalogLoadedMsg{
		Token: token,
		Rows: []RepoEntry{
			{Namespace: "acme", Repo: "gateway"},
			{Namespace: "acme", Repo: "platform"},
			{Namespace: "beta", Repo: "payments"},
		},
	})
	model = updated.(*rootModel)

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(*rootModel)
	if model.namespaces.Selected != 1 {
		t.Fatalf("expected selection to move to beta, got %d", model.namespaces.Selected)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
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

func TestRootModelEscReturnsFromReposToNamespaces(t *testing.T) {
	t.Parallel()

	model := newRootModel(&fakeBrowserService{}, InitialRoute{Kind: RouteRepos, Namespace: "acme"}, RequestOptions{})
	token := model.beginRepoCatalogLoad()

	updated, _ := model.Update(repoCatalogLoadedMsg{
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

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
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
	token := model.beginRepoCatalogLoad()

	updated, _ := model.Update(repoCatalogLoadedMsg{Token: token})
	model = updated.(*rootModel)

	if got := model.View(); !strings.Contains(got, "No namespaces found.") {
		t.Fatalf("expected empty namespace state, got %q", got)
	}
}

func TestRootModelRendersStartupLoadFailure(t *testing.T) {
	t.Parallel()

	model := newRootModel(&fakeBrowserService{}, InitialRoute{Kind: RouteNamespaces}, RequestOptions{})
	token := model.beginRepoCatalogLoad()

	updated, _ := model.Update(loadFailedMsg{
		Domain: loadDomainRepoCatalog,
		Token:  token,
		Err:    errors.New("catalog unavailable"),
	})
	model = updated.(*rootModel)

	view := model.View()
	if !strings.Contains(view, "Failed to load repositories.") {
		t.Fatalf("expected failure heading, got %q", view)
	}
	if !strings.Contains(view, "catalog unavailable") {
		t.Fatalf("expected failure cause, got %q", view)
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

type fakeBrowserService struct {
	listReposBody      []byte
	listReposErr       error
	listOperationsBody []byte
	listOperationsErr  error
	operationBody      []byte
	operationErr       error
	specBody           []byte
	specErr            error
	lastContextValue   any
}

func (service *fakeBrowserService) ListRepos(
	ctx context.Context,
	options RequestOptions,
	format clioutput.ListFormat,
) ([]byte, error) {
	service.lastContextValue = ctx.Value(contextKey("request"))
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
	_ = selector
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
	_ = selector
	_ = options
	_ = format
	return service.specBody, service.specErr
}

type contextKey string
