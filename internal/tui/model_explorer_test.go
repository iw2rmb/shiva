package tui

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestRootModelEnterRepoStartsExplorerOperationLoad(t *testing.T) {
	t.Parallel()

	operationRows := []map[string]any{
		{"namespace": "acme", "repo": "platform", "api": "z.yaml", "method": "get", "path": "/pets", "operation_id": "b"},
		{"namespace": "acme", "repo": "platform", "api": "a.yaml", "method": "get", "path": "/pets", "operation_id": "a"},
		{"namespace": "acme", "repo": "platform", "api": "a.yaml", "method": "post", "path": "/pets", "operation_id": "createPet"},
		{"namespace": "acme", "repo": "platform", "api": "a.yaml", "method": "get", "path": "/accounts", "operation_id": "listAccounts"},
	}
	operationBody, err := json.Marshal(operationRows)
	if err != nil {
		t.Fatalf("marshal operation rows: %v", err)
	}

	service := &fakeBrowserService{listOperationsBody: operationBody}
	model := newRootModel(service, InitialRoute{Kind: RouteRepos, Namespace: "acme"}, RequestOptions{})

	repoToken := model.beginRepoCatalogLoad()
	updated, _ := model.Update(repoCatalogLoadedMsg{
		Token: repoToken,
		Rows:  []RepoEntry{{Namespace: "acme", Repo: "platform"}},
	})
	model = updated.(*rootModel)

	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(*rootModel)

	if model.activeRoute != RouteRepoExplorer {
		t.Fatalf("expected active route %q, got %q", RouteRepoExplorer, model.activeRoute)
	}
	if cmd == nil {
		t.Fatalf("expected operation list load command")
	}
	if service.listOperationsCall != 0 {
		t.Fatalf("expected command to defer execution, got %d calls", service.listOperationsCall)
	}

	msg := cmd()
	loaded, ok := msg.(operationListLoadedMsg)
	if !ok {
		t.Fatalf("expected operationListLoadedMsg, got %T", msg)
	}
	if service.listOperationsCall != 1 {
		t.Fatalf("expected one operation list call, got %d", service.listOperationsCall)
	}
	if service.lastOperationQuery.Namespace != "acme" || service.lastOperationQuery.Repo != "platform" {
		t.Fatalf("unexpected selector %+v", service.lastOperationQuery)
	}
	if service.lastOperationQuery.API != "" {
		t.Fatalf("expected repo-level operation list without API filter, got %q", service.lastOperationQuery.API)
	}
	if loaded.Token != model.async.OperationList.ActiveToken {
		t.Fatalf("expected token %d, got %d", model.async.OperationList.ActiveToken, loaded.Token)
	}

	updated, _ = model.Update(loaded)
	model = updated.(*rootModel)

	if model.explorer.Selected != 0 {
		t.Fatalf("expected first endpoint selected, got %d", model.explorer.Selected)
	}
	gotOrder := []EndpointIdentity{
		model.explorer.Endpoints[0].Identity,
		model.explorer.Endpoints[1].Identity,
		model.explorer.Endpoints[2].Identity,
		model.explorer.Endpoints[3].Identity,
	}
	wantOrder := []EndpointIdentity{
		{Namespace: "acme", Repo: "platform", API: "a.yaml", Method: "get", Path: "/accounts", OperationID: "listAccounts"},
		{Namespace: "acme", Repo: "platform", API: "a.yaml", Method: "get", Path: "/pets", OperationID: "a"},
		{Namespace: "acme", Repo: "platform", API: "z.yaml", Method: "get", Path: "/pets", OperationID: "b"},
		{Namespace: "acme", Repo: "platform", API: "a.yaml", Method: "post", Path: "/pets", OperationID: "createPet"},
	}
	if !reflect.DeepEqual(gotOrder, wantOrder) {
		t.Fatalf("unexpected endpoint sort order:\nwant: %+v\ngot:  %+v", wantOrder, gotOrder)
	}
}

func TestRootModelExplorerArrowKeysUpdatePlaceholderIdentity(t *testing.T) {
	t.Parallel()

	model := newRootModel(&fakeBrowserService{}, InitialRoute{
		Kind:      RouteRepoExplorer,
		Namespace: "acme",
		Repo:      "platform",
	}, RequestOptions{})

	token := model.beginOperationListLoad()
	updated, _ := model.Update(operationListLoadedMsg{
		Token: token,
		Entries: []EndpointEntry{
			{Identity: EndpointIdentity{Namespace: "acme", Repo: "platform", API: "a.yaml", Method: "get", Path: "/beta", OperationID: "beta"}},
			{Identity: EndpointIdentity{Namespace: "acme", Repo: "platform", API: "a.yaml", Method: "get", Path: "/alpha", OperationID: "alpha"}},
		},
	})
	model = updated.(*rootModel)

	if model.explorer.Selected != 0 {
		t.Fatalf("expected first endpoint selected, got %d", model.explorer.Selected)
	}
	if got := model.View().Content; !strings.Contains(got, "path: /alpha") {
		t.Fatalf("expected placeholder for first endpoint, got %q", got)
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	model = updated.(*rootModel)
	if model.explorer.Selected != 1 {
		t.Fatalf("expected selection to move down to 1, got %d", model.explorer.Selected)
	}
	if got := model.View().Content; !strings.Contains(got, "path: /beta") {
		t.Fatalf("expected placeholder to follow moved selection, got %q", got)
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	model = updated.(*rootModel)
	if model.explorer.Selected != 0 {
		t.Fatalf("expected selection to move up to 0, got %d", model.explorer.Selected)
	}
	if got := model.View().Content; !strings.Contains(got, "path: /alpha") {
		t.Fatalf("expected placeholder to follow selection up, got %q", got)
	}
}

func TestRootModelExplorerEscReturnsToRepoList(t *testing.T) {
	t.Parallel()

	model := newRootModel(&fakeBrowserService{}, InitialRoute{Kind: RouteRepos, Namespace: "acme"}, RequestOptions{})
	repoToken := model.beginRepoCatalogLoad()
	updated, _ := model.Update(repoCatalogLoadedMsg{
		Token: repoToken,
		Rows:  []RepoEntry{{Namespace: "acme", Repo: "platform"}},
	})
	model = updated.(*rootModel)

	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(*rootModel)
	if cmd == nil {
		t.Fatalf("expected operation list command")
	}
	updated, _ = model.Update(cmd())
	model = updated.(*rootModel)

	if model.activeRoute != RouteRepoExplorer {
		t.Fatalf("expected route %q, got %q", RouteRepoExplorer, model.activeRoute)
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	model = updated.(*rootModel)

	if model.activeRoute != RouteRepos {
		t.Fatalf("expected route %q, got %q", RouteRepos, model.activeRoute)
	}
	if model.repoList.Namespace != "acme" || model.repoList.Selected != 0 {
		t.Fatalf("expected repo list state preserved, got namespace=%q selected=%d", model.repoList.Namespace, model.repoList.Selected)
	}
}

func TestRootModelExplorerRendersEmptyOperationCatalog(t *testing.T) {
	t.Parallel()

	model := newRootModel(&fakeBrowserService{}, InitialRoute{
		Kind:      RouteRepoExplorer,
		Namespace: "acme",
		Repo:      "platform",
	}, RequestOptions{})

	token := model.beginOperationListLoad()
	updated, _ := model.Update(operationListLoadedMsg{Token: token})
	model = updated.(*rootModel)

	if model.explorer.Selected != -1 {
		t.Fatalf("expected no selected endpoint, got %d", model.explorer.Selected)
	}
	if got := model.View().Content; !strings.Contains(got, "No endpoints found in repository.") {
		t.Fatalf("expected empty operation catalog message, got %q", got)
	}
}
