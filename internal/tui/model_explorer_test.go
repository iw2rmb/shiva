package tui

import (
	"encoding/json"
	"errors"
	"reflect"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	clioutput "github.com/iw2rmb/shiva/internal/cli/output"
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
		Rows: []RepoEntry{{
			Namespace: "acme",
			Repo:      "platform",
			Row: clioutput.RepoRow{
				Namespace:        "acme",
				Repo:             "platform",
				ActiveAPICount:   1,
				SnapshotRevision: &clioutput.RevisionState{ID: 42, SHA: "deadbeef"},
			},
		}},
	})
	model = updated.(*rootModel)

	next, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(*rootModel)

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

	var loaded operationListLoadedMsg
	found := false
	for _, msg := range collectCmdMessages(cmd) {
		typed, ok := msg.(operationListLoadedMsg)
		if !ok {
			continue
		}
		loaded = typed
		found = true
		break
	}
	if !found {
		t.Fatalf("expected operationListLoadedMsg in command batch")
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

func TestRootModelEnterRepoWithoutSnapshotSkipsOperationLoad(t *testing.T) {
	t.Parallel()

	service := &fakeBrowserService{}
	model := newRootModel(service, InitialRoute{Kind: RouteRepos, Namespace: "acme"}, RequestOptions{})

	repoToken := model.beginRepoCatalogLoad()
	updated, _ := model.Update(repoCatalogLoadedMsg{
		Token: repoToken,
		Rows: []RepoEntry{{
			Namespace: "acme",
			Repo:      "platform",
			Row: clioutput.RepoRow{
				Namespace:        "acme",
				Repo:             "platform",
				ActiveAPICount:   0,
				SnapshotRevision: nil,
			},
		}},
	})
	model = updated.(*rootModel)

	next, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(*rootModel)

	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(*rootModel)

	if model.activeRoute != RouteRepoExplorer {
		t.Fatalf("expected active route %q, got %q", RouteRepoExplorer, model.activeRoute)
	}
	for _, msg := range collectCmdMessages(cmd) {
		if _, ok := msg.(operationListLoadedMsg); ok {
			t.Fatalf("expected operation list load command to be skipped")
		}
	}
	if service.listOperationsCall != 0 {
		t.Fatalf("expected no operation list calls, got %d", service.listOperationsCall)
	}
	if model.async.OperationList.Loading {
		t.Fatalf("expected operation list loading to remain false")
	}

	got := stripANSI(model.View().Content)
	if !strings.Contains(got, "No endpoints found for current scope.") {
		t.Fatalf("expected empty operation catalog message, got %q", got)
	}
}

func TestRootModelExplorerArrowKeysUpdateSelection(t *testing.T) {
	t.Parallel()

	model := newRootModel(&fakeBrowserService{}, InitialRoute{
		Kind:      RouteRepoExplorer,
		Namespace: "acme",
		Repo:      "platform",
	}, RequestOptions{})
	if model.activeRoute == RouteHome {
		updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		model = updated.(*rootModel)
	}

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

	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	model = updated.(*rootModel)
	if model.explorer.Selected != 1 {
		t.Fatalf("expected selection to move down to 1, got %d", model.explorer.Selected)
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	model = updated.(*rootModel)
	if model.explorer.Selected != 0 {
		t.Fatalf("expected selection to move up to 0, got %d", model.explorer.Selected)
	}
}

func TestRootModelExplorerEscReturnsToRepoList(t *testing.T) {
	t.Parallel()

	model := newRootModel(&fakeBrowserService{}, InitialRoute{Kind: RouteRepos, Namespace: "acme"}, RequestOptions{})
	repoToken := model.beginRepoCatalogLoad()
	updated, _ := model.Update(repoCatalogLoadedMsg{
		Token: repoToken,
		Rows: []RepoEntry{{
			Namespace: "acme",
			Repo:      "platform",
			Row: clioutput.RepoRow{
				Namespace:        "acme",
				Repo:             "platform",
				ActiveAPICount:   1,
				SnapshotRevision: &clioutput.RevisionState{ID: 42, SHA: "deadbeef"},
			},
		}},
	})
	model = updated.(*rootModel)

	next, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(*rootModel)

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

	if model.activeRoute != RouteHome {
		t.Fatalf("expected route %q, got %q", RouteHome, model.activeRoute)
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
	got := stripANSI(model.View().Content)
	if !strings.Contains(got, "No endpoints found for current scope.") {
		t.Fatalf("expected empty operation catalog message, got %q", got)
	}
}

func TestRootModelExplorerLoadsSelectedEndpointDetailUsingExactIdentity(t *testing.T) {
	t.Parallel()

	service := &fakeBrowserService{
		operationBody: []byte(`{"operationId":"listAccounts"}`),
	}
	model := newRootModel(service, InitialRoute{
		Kind:      RouteRepoExplorer,
		Namespace: "acme",
		Repo:      "platform",
	}, RequestOptions{})

	token := model.beginOperationListLoad()
	updated, cmd := model.Update(operationListLoadedMsg{
		Token: token,
		Entries: []EndpointEntry{
			{Identity: EndpointIdentity{Namespace: "acme", Repo: "platform", API: "b.yaml", Method: "get", Path: "/pets", OperationID: "listPets"}},
			{Identity: EndpointIdentity{Namespace: "acme", Repo: "platform", API: "a.yaml", Method: "get", Path: "/accounts", OperationID: "listAccounts"}},
		},
	})
	model = updated.(*rootModel)

	if cmd == nil {
		t.Fatalf("expected operation detail load command")
	}

	selected, ok := model.explorer.SelectedEndpoint()
	if !ok {
		t.Fatalf("expected selected endpoint")
	}

	var msg tea.Msg
	var loaded operationDetailLoadedMsg
	found := false
	for _, candidate := range collectCmdMessages(cmd) {
		typed, ok := candidate.(operationDetailLoadedMsg)
		if !ok {
			continue
		}
		loaded = typed
		msg = candidate
		found = true
		break
	}
	if !found {
		t.Fatalf("expected operationDetailLoadedMsg in command batch")
	}

	if service.getOperationCall != 1 {
		t.Fatalf("expected one operation detail call, got %d", service.getOperationCall)
	}
	if loaded.Detail.Endpoint != selected.Identity {
		t.Fatalf("expected loaded endpoint %+v, got %+v", selected.Identity, loaded.Detail.Endpoint)
	}
	if service.lastOperationGet.Namespace != selected.Identity.Namespace ||
		service.lastOperationGet.Repo != selected.Identity.Repo ||
		service.lastOperationGet.API != selected.Identity.API ||
		service.lastOperationGet.OperationID != selected.Identity.OperationID ||
		service.lastOperationGet.Method != selected.Identity.Method ||
		service.lastOperationGet.Path != selected.Identity.Path {
		t.Fatalf("expected exact selected endpoint selector, got %+v", service.lastOperationGet)
	}

	updated, specCmd := model.Update(msg)
	model = updated.(*rootModel)
	if specCmd != nil {
		t.Fatalf("expected endpoints tab load to skip spec command")
	}
	if model.explorer.Detail.Operation == nil {
		t.Fatalf("expected operation detail to be set")
	}
}

func TestRootModelUnscopedOperationsSelectionUsesRowIdentityForDetails(t *testing.T) {
	t.Parallel()

	service := &fakeBrowserService{
		operationBody: []byte(`{"operationId":"listPets"}`),
	}
	model := newRootModel(service, InitialRoute{Kind: RouteHome}, RequestOptions{})
	model.setHomeSelection(homeItemEndpoints)

	token := model.beginOperationListLoad()
	updated, cmd := model.Update(operationListLoadedMsg{
		Token: token,
		Entries: []EndpointEntry{
			{
				Identity: EndpointIdentity{
					Namespace:   "acme",
					Repo:        "platform",
					API:         "pets.yaml",
					Method:      "get",
					Path:        "/pets",
					OperationID: "listPets",
				},
			},
		},
	})
	model = updated.(*rootModel)

	if cmd == nil {
		t.Fatalf("expected operation detail load command")
	}
	var msg tea.Msg
	found := false
	for _, candidate := range collectCmdMessages(cmd) {
		if _, ok := candidate.(operationDetailLoadedMsg); !ok {
			continue
		}
		msg = candidate
		found = true
		break
	}
	if !found {
		t.Fatalf("expected operationDetailLoadedMsg in command batch")
	}
	if service.getOperationCall != 1 {
		t.Fatalf("expected one operation detail call, got %d", service.getOperationCall)
	}
	if service.lastOperationGet.Namespace != "acme" || service.lastOperationGet.Repo != "platform" {
		t.Fatalf("expected operation detail selector namespace/repo from row identity, got %+v", service.lastOperationGet)
	}

	updated, _ = model.Update(msg)
	model = updated.(*rootModel)
	if model.explorer.Detail.Operation == nil {
		t.Fatalf("expected operation detail to be set")
	}
}

func TestRootModelExplorerLoadsOnlyOperationDetailAcrossTabs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		tab           DetailTab
		operationBody []byte
	}{
		{
			name:          "response tab",
			tab:           DetailTabResponse,
			operationBody: []byte(`{"operationId":"listPets","responses":{"200":{"description":"ok"}}}`),
		},
		{
			name:          "request tab",
			tab:           DetailTabRequest,
			operationBody: []byte(`{"operationId":"listPets"}`),
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			service := &fakeBrowserService{
				operationBody: tc.operationBody,
			}
			model, _ := newExplorerModelWithSingleEndpoint(service, tc.tab)

			cmd := model.loadExplorerDetailForSelection()
			if cmd == nil {
				t.Fatalf("expected operation load command")
			}
			operationMsg := cmd()
			updated, followCmd := model.Update(operationMsg)
			model = updated.(*rootModel)
			if followCmd != nil {
				t.Fatalf("expected no follow-up detail command")
			}
			if service.getSpecCall != 0 {
				t.Fatalf("expected no spec calls, got %d", service.getSpecCall)
			}
			if model.explorer.Detail.Operation == nil {
				t.Fatalf("expected operation detail to be set")
			}
		})
	}
}

func TestRootModelExplorerDetailLoadUsesSessionCaches(t *testing.T) {
	t.Parallel()

	service := &fakeBrowserService{
		operationBody: []byte(`{"operationId":"listPets","responses":{"200":{"description":"ok"}}}`),
	}
	model, _ := newExplorerModelWithSingleEndpoint(service, DetailTabResponse)

	operationCmd := model.loadExplorerDetailForSelection()
	if operationCmd == nil {
		t.Fatalf("expected initial operation detail command")
	}
	operationMsg := operationCmd()
	updated, followCmd := model.Update(operationMsg)
	model = updated.(*rootModel)
	if followCmd != nil {
		t.Fatalf("expected no follow-up detail command")
	}
	if service.getOperationCall != 1 {
		t.Fatalf("expected one operation call, got %d", service.getOperationCall)
	}

	model.clearExplorerDetailState()
	reloadCmd := model.loadExplorerDetailForSelection()
	if reloadCmd != nil {
		t.Fatalf("expected cache hit to avoid network commands")
	}
	if model.explorer.Detail.Operation == nil {
		t.Fatalf("expected cached operation detail")
	}
	if service.getOperationCall != 1 {
		t.Fatalf("expected cache replay to avoid extra calls, got operation=%d", service.getOperationCall)
	}
}

func TestRootModelExplorerIgnoresStaleOperationResponseAfterRapidSelectionChange(t *testing.T) {
	t.Parallel()

	service := &fakeBrowserService{
		operationBody: []byte(`{"operationId":"detail"}`),
	}
	model := newRootModel(service, InitialRoute{
		Kind:      RouteRepoExplorer,
		Namespace: "acme",
		Repo:      "platform",
	}, RequestOptions{})
	model.explorer.Endpoints = []EndpointEntry{
		{Identity: EndpointIdentity{Namespace: "acme", Repo: "platform", API: "pets.yaml", Method: "get", Path: "/a", OperationID: "old"}},
		{Identity: EndpointIdentity{Namespace: "acme", Repo: "platform", API: "pets.yaml", Method: "get", Path: "/b", OperationID: "new"}},
	}
	model.refreshExplorerList()
	if model.explorer.Selected != 0 {
		t.Fatalf("expected first endpoint selected, got %d", model.explorer.Selected)
	}

	staleCmd := model.loadExplorerDetailForSelection()
	if staleCmd == nil {
		t.Fatalf("expected stale candidate operation command")
	}

	model.explorer.List.Select(1)
	currentCmd := model.syncExplorerSelection()
	if currentCmd == nil {
		t.Fatalf("expected current operation command after selection change")
	}
	if model.explorer.Selected != 1 {
		t.Fatalf("expected selection on new endpoint, got %d", model.explorer.Selected)
	}

	staleMsg := staleCmd()
	updated, _ := model.Update(staleMsg)
	model = updated.(*rootModel)
	if model.explorer.Detail.Operation != nil {
		t.Fatalf("expected stale operation response to be ignored, got %+v", model.explorer.Detail.Operation)
	}

	currentMsg := currentCmd()
	updated, _ = model.Update(currentMsg)
	model = updated.(*rootModel)
	if model.explorer.Detail.Operation == nil {
		t.Fatalf("expected current operation response to apply")
	}
	if got := model.explorer.Detail.Operation.Endpoint.OperationID; got != "new" {
		t.Fatalf("expected operation detail for new endpoint, got %q", got)
	}
}

func TestRootModelExplorerTabSwitchesReplaceViewportContent(t *testing.T) {
	t.Parallel()

	model, _ := newExplorerModelWithSingleEndpoint(&fakeBrowserService{}, DetailTabRequest)
	model.explorer.Detail.Operation = &OperationDetail{
		Endpoint: model.explorer.Endpoints[0].Identity,
		Body: json.RawMessage(`{
			"operationId":"listPets",
			"summary":"List pets",
			"responses":{"200":{"description":"ok"},"400":{"description":"bad request"}},
			"servers":[{"url":"https://operation.example"}]
		}`),
	}
	model.refreshExplorerDetailViewport()

	endpointRendered := stripANSI(model.explorer.Detail.Viewport.GetContent())
	if !strings.Contains(endpointRendered, "Operation ID:") {
		t.Fatalf("expected request markdown content, got %q", endpointRendered)
	}
	selectionBefore := model.explorer.Selected

	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = updated.(*rootModel)
	if model.explorer.Detail.ActiveTab != DetailTabResponse {
		t.Fatalf("expected active tab %q, got %q", DetailTabResponse, model.explorer.Detail.ActiveTab)
	}
	if model.explorer.Selected != selectionBefore {
		t.Fatalf("expected endpoint selection unchanged, got %d", model.explorer.Selected)
	}
	responseRendered := stripANSI(model.explorer.Detail.Viewport.GetContent())
	if !strings.Contains(responseRendered, "Responses") {
		t.Fatalf("expected response markdown content, got %q", responseRendered)
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	model = updated.(*rootModel)
	if model.explorer.Detail.ActiveTab != DetailTabRequest {
		t.Fatalf("expected active tab %q, got %q", DetailTabRequest, model.explorer.Detail.ActiveTab)
	}
}

func TestRenderExplorerPanesSwitchesBetweenStackedAndSplitLayouts(t *testing.T) {
	t.Parallel()

	styles := newTUIStyles()
	stacked := stripANSI(renderExplorerPanes(styles, "left-content", "right-content", 60))
	split := stripANSI(renderExplorerPanes(styles, "left-content", "right-content", 120))

	if !strings.Contains(stacked, "left-content") || !strings.Contains(stacked, "right-content") {
		t.Fatalf("expected stacked layout to include both pane contents, got %q", stacked)
	}
	if !strings.Contains(stacked, "\n\nDetails\n") {
		t.Fatalf("expected stacked layout to place details pane below endpoints pane, got %q", stacked)
	}
	if strings.Contains(split, "\n\nDetails\n") {
		t.Fatalf("expected split layout to avoid stacked separator, got %q", split)
	}
}

func TestRootModelExplorerViewportScrollableWithPageDown(t *testing.T) {
	t.Parallel()

	model, selected := newExplorerModelWithSingleEndpoint(&fakeBrowserService{}, DetailTabRequest)
	lines := make([]string, 0, 80)
	for i := 0; i < 80; i++ {
		lines = append(lines, "line")
	}
	model.explorer.Detail.Operation = &OperationDetail{
		Endpoint: selected,
		Body: json.RawMessage(`{
			"operationId":"listPets",
			"description":"` + strings.Join(lines, `\n`) + `"
		}`),
	}
	model.explorer.Detail.Viewport.SetHeight(8)
	model.refreshExplorerDetailViewport()
	if model.explorer.Detail.Viewport.YOffset() != 0 {
		t.Fatalf("expected viewport to start at top")
	}

	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	model = updated.(*rootModel)
	if model.explorer.Detail.Viewport.YOffset() == 0 {
		t.Fatalf("expected viewport y-offset to increase after page down")
	}
}

func TestRootModelExplorerResizeRerendersUsingViewportWidth(t *testing.T) {
	t.Parallel()

	model, selected := newExplorerModelWithSingleEndpoint(&fakeBrowserService{}, DetailTabRequest)
	model.explorer.Detail.Operation = &OperationDetail{
		Endpoint: selected,
		Body: json.RawMessage(`{
			"operationId":"listPets",
			"description":"This is a long markdown paragraph that should wrap differently once viewport width changes significantly."
		}`),
	}

	updated, _ := model.Update(resizeMsg{Width: 80, Height: 24})
	model = updated.(*rootModel)
	narrowRendered := stripANSI(model.explorer.Detail.Viewport.GetContent())
	narrowWidth := model.explorer.Detail.Viewport.Width()

	updated, _ = model.Update(resizeMsg{Width: 140, Height: 24})
	model = updated.(*rootModel)
	wideRendered := stripANSI(model.explorer.Detail.Viewport.GetContent())
	wideWidth := model.explorer.Detail.Viewport.Width()

	if wideWidth == narrowWidth {
		t.Fatalf("expected detail viewport width to change on resize, narrow=%d wide=%d", narrowWidth, wideWidth)
	}
	if narrowRendered == wideRendered {
		t.Fatalf("expected rendered markdown to change with viewport width")
	}
}

func TestRootModelExplorerShowsOperationDetailLoadErrorInDetailsPane(t *testing.T) {
	t.Parallel()

	model, _ := newExplorerModelWithSingleEndpoint(&fakeBrowserService{}, DetailTabRequest)
	token := model.beginOperationDetailLoad()
	updated, _ := model.Update(loadFailedMsg{
		Domain: loadDomainOperationDetail,
		Token:  token,
		Err:    errors.New("operation detail request failed"),
	})
	model = updated.(*rootModel)

	rendered := normalizeViewportText(stripANSI(model.explorer.Detail.Viewport.GetContent()))
	if !strings.Contains(rendered, "Failed to load detail: operation detail request failed") {
		t.Fatalf("expected operation detail load error in details pane, got %q", rendered)
	}
}

func TestRootModelExplorerShowsResponseDetailLoadErrorInResponseTab(t *testing.T) {
	t.Parallel()

	model, _ := newExplorerModelWithSingleEndpoint(&fakeBrowserService{}, DetailTabResponse)
	token := model.beginOperationDetailLoad()
	updated, _ := model.Update(loadFailedMsg{
		Domain: loadDomainOperationDetail,
		Token:  token,
		Err:    errors.New("response detail request failed"),
	})
	model = updated.(*rootModel)

	rendered := normalizeViewportText(stripANSI(model.explorer.Detail.Viewport.GetContent()))
	if !strings.Contains(rendered, "Failed to load detail: response detail request failed") {
		t.Fatalf("expected response detail load error in response pane, got %q", rendered)
	}
}

func newExplorerModelWithSingleEndpoint(
	service *fakeBrowserService,
	tab DetailTab,
) (*rootModel, EndpointIdentity) {
	model := newRootModel(service, InitialRoute{
		Kind:      RouteRepoExplorer,
		Namespace: "acme",
		Repo:      "platform",
	}, RequestOptions{})
	selected := EndpointIdentity{
		Namespace:   "acme",
		Repo:        "platform",
		API:         "pets.yaml",
		Method:      "get",
		Path:        "/pets",
		OperationID: "listPets",
	}
	model.explorer.Endpoints = []EndpointEntry{{Identity: selected}}
	model.refreshExplorerList()
	model.explorer.Detail.ActiveTab = tab
	model.refreshExplorerDetailViewport()
	if model.activeRoute == RouteHome {
		updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		model = updated.(*rootModel)
	}
	return model, selected
}

func stripANSI(value string) string {
	re := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	return re.ReplaceAllString(value, "")
}

func normalizeViewportText(value string) string {
	value = strings.ReplaceAll(value, "\u00a0", " ")
	value = strings.Join(strings.Fields(value), " ")
	return value
}
