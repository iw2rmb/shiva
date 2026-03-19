package tui

import (
	"encoding/json"
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

	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(*rootModel)

	if model.activeRoute != RouteRepoExplorer {
		t.Fatalf("expected active route %q, got %q", RouteRepoExplorer, model.activeRoute)
	}
	if cmd != nil {
		t.Fatalf("expected operation list load command to be skipped")
	}
	if service.listOperationsCall != 0 {
		t.Fatalf("expected no operation list calls, got %d", service.listOperationsCall)
	}
	if model.async.OperationList.Loading {
		t.Fatalf("expected operation list loading to remain false")
	}

	got := stripANSI(model.View().Content)
	if !strings.Contains(got, "No endpoints found in") || !strings.Contains(got, "repository.") {
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
	got := stripANSI(model.View().Content)
	if !strings.Contains(got, "No endpoints found in") || !strings.Contains(got, "repository.") {
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

	msg := cmd()
	loaded, ok := msg.(operationDetailLoadedMsg)
	if !ok {
		t.Fatalf("expected operationDetailLoadedMsg, got %T", msg)
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

func TestRootModelExplorerLazySpecLoadForServersTab(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		tab            DetailTab
		operationBody  []byte
		expectSpecLoad bool
	}{
		{
			name:           "servers tab loads spec when operation servers missing",
			tab:            DetailTabServers,
			operationBody:  []byte(`{"operationId":"listPets"}`),
			expectSpecLoad: true,
		},
		{
			name:           "servers tab loads spec when operation servers empty",
			tab:            DetailTabServers,
			operationBody:  []byte(`{"operationId":"listPets","servers":[]}`),
			expectSpecLoad: true,
		},
		{
			name:           "servers tab skips spec when operation servers present",
			tab:            DetailTabServers,
			operationBody:  []byte(`{"operationId":"listPets","servers":[{"url":"https://operation.example"}]}`),
			expectSpecLoad: false,
		},
		{
			name:           "endpoints tab skips spec when operation servers missing",
			tab:            DetailTabEndpoints,
			operationBody:  []byte(`{"operationId":"listPets"}`),
			expectSpecLoad: false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			service := &fakeBrowserService{
				operationBody: tc.operationBody,
				specBody:      []byte(`{"openapi":"3.1.0","servers":[{"url":"https://spec.example"}]}`),
			}
			model, selected := newExplorerModelWithSingleEndpoint(service, tc.tab)

			cmd := model.loadExplorerDetailForSelection()
			if cmd == nil {
				t.Fatalf("expected operation load command")
			}
			operationMsg := cmd()
			updated, followCmd := model.Update(operationMsg)
			model = updated.(*rootModel)

			if tc.expectSpecLoad {
				if followCmd == nil {
					t.Fatalf("expected lazy spec load command")
				}
				specMsg := followCmd()
				if _, ok := specMsg.(specDetailLoadedMsg); !ok {
					t.Fatalf("expected specDetailLoadedMsg, got %T", specMsg)
				}
				updated, _ = model.Update(specMsg)
				model = updated.(*rootModel)

				if service.getSpecCall != 1 {
					t.Fatalf("expected one spec call, got %d", service.getSpecCall)
				}
				if service.lastSpecFormat != SpecFormatJSON {
					t.Fatalf("expected spec format %q, got %q", SpecFormatJSON, service.lastSpecFormat)
				}
				if service.lastSpecGet.Namespace != selected.Namespace ||
					service.lastSpecGet.Repo != selected.Repo ||
					service.lastSpecGet.API != selected.API {
					t.Fatalf("unexpected spec selector %+v", service.lastSpecGet)
				}
				if model.explorer.Detail.Spec == nil {
					t.Fatalf("expected spec detail to be set")
				}
				return
			}

			if followCmd != nil {
				t.Fatalf("expected no spec load command")
			}
			if service.getSpecCall != 0 {
				t.Fatalf("expected no spec calls, got %d", service.getSpecCall)
			}
			if model.explorer.Detail.Spec != nil {
				t.Fatalf("expected no spec detail, got %+v", model.explorer.Detail.Spec)
			}
		})
	}
}

func TestRootModelExplorerDetailLoadUsesSessionCaches(t *testing.T) {
	t.Parallel()

	service := &fakeBrowserService{
		operationBody: []byte(`{"operationId":"listPets","servers":[]}`),
		specBody:      []byte(`{"openapi":"3.1.0","servers":[{"url":"https://spec.example"}]}`),
	}
	model, _ := newExplorerModelWithSingleEndpoint(service, DetailTabServers)

	operationCmd := model.loadExplorerDetailForSelection()
	if operationCmd == nil {
		t.Fatalf("expected initial operation detail command")
	}
	operationMsg := operationCmd()
	updated, specCmd := model.Update(operationMsg)
	model = updated.(*rootModel)
	if specCmd == nil {
		t.Fatalf("expected initial spec detail command")
	}

	specMsg := specCmd()
	updated, _ = model.Update(specMsg)
	model = updated.(*rootModel)

	if service.getOperationCall != 1 || service.getSpecCall != 1 {
		t.Fatalf("expected one operation and one spec call, got operation=%d spec=%d", service.getOperationCall, service.getSpecCall)
	}

	model.clearExplorerDetailState()
	reloadCmd := model.loadExplorerDetailForSelection()
	if reloadCmd != nil {
		t.Fatalf("expected cache hit to avoid network commands")
	}
	if model.explorer.Detail.Operation == nil {
		t.Fatalf("expected cached operation detail")
	}
	if model.explorer.Detail.Spec == nil {
		t.Fatalf("expected cached spec detail")
	}
	if service.getOperationCall != 1 || service.getSpecCall != 1 {
		t.Fatalf("expected cache replay to avoid extra calls, got operation=%d spec=%d", service.getOperationCall, service.getSpecCall)
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

	model, _ := newExplorerModelWithSingleEndpoint(&fakeBrowserService{}, DetailTabEndpoints)
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
	if !strings.Contains(endpointRendered, "GET /pets") {
		t.Fatalf("expected endpoint markdown content, got %q", endpointRendered)
	}
	selectionBefore := model.explorer.Selected

	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = updated.(*rootModel)
	if model.explorer.Detail.ActiveTab != DetailTabServers {
		t.Fatalf("expected active tab %q, got %q", DetailTabServers, model.explorer.Detail.ActiveTab)
	}
	if model.explorer.Selected != selectionBefore {
		t.Fatalf("expected endpoint selection unchanged, got %d", model.explorer.Selected)
	}
	serversRendered := stripANSI(model.explorer.Detail.Viewport.GetContent())
	if !strings.Contains(serversRendered, "Servers") {
		t.Fatalf("expected servers markdown content, got %q", serversRendered)
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	model = updated.(*rootModel)
	if model.explorer.Detail.ActiveTab != DetailTabEndpoints {
		t.Fatalf("expected active tab %q, got %q", DetailTabEndpoints, model.explorer.Detail.ActiveTab)
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

	model, selected := newExplorerModelWithSingleEndpoint(&fakeBrowserService{}, DetailTabEndpoints)
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

	model, selected := newExplorerModelWithSingleEndpoint(&fakeBrowserService{}, DetailTabEndpoints)
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

	if wideWidth <= narrowWidth {
		t.Fatalf("expected detail viewport width to increase on resize, narrow=%d wide=%d", narrowWidth, wideWidth)
	}
	if narrowRendered == wideRendered {
		t.Fatalf("expected rendered markdown to change with viewport width")
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
	return model, selected
}

func stripANSI(value string) string {
	re := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	return re.ReplaceAllString(value, "")
}
