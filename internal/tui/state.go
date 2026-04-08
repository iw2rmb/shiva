package tui

import (
	"encoding/json"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/paginator"
	"charm.land/bubbles/v2/viewport"
	clioutput "github.com/iw2rmb/shiva/internal/cli/output"
)

type DetailTab string

const (
	DetailTabRequest  DetailTab = "request"
	DetailTabResponse DetailTab = "response"
	DetailTabErrors   DetailTab = "errors"
)

type NamespaceEntry struct {
	Namespace  string
	RepoCount  int
	AllPending bool
}

type HomeEntry struct {
	Title       string
	Description string
	Route       RouteKind
}

type RepoEntry struct {
	Namespace string
	Repo      string
	Row       clioutput.RepoRow
}

type APIEntry struct {
	Namespace string
	Repo      string
	Title     string
	API       string
	Row       clioutput.APIRow
}

type EndpointIdentity struct {
	Namespace   string
	Repo        string
	API         string
	OperationID string
	Method      string
	Path        string
}

type EndpointEntry struct {
	Identity EndpointIdentity
	Row      clioutput.OperationRow
}

type OperationDetail struct {
	Endpoint EndpointIdentity
	Body     json.RawMessage
}

type SpecDetail struct {
	Namespace string
	Repo      string
	API       string
	Revision  int64
	SHA       string
	Body      json.RawMessage
}

type DetailState struct {
	ActiveTab DetailTab
	Operation *OperationDetail
	Spec      *SpecDetail
	Viewport  viewport.Model
}

type SpecIdentity struct {
	Namespace string
	Repo      string
	API       string
}

type NamespaceRouteState struct {
	Entries  []NamespaceEntry
	Selected int
	List     list.Model
	Pager    paginator.Model
	Query    string
}

type HomeRouteState struct {
	Entries  []HomeEntry
	Selected int
	List     list.Model
}

type RepoRouteState struct {
	Namespace string
	Entries   []RepoEntry
	Selected  int
	List      list.Model
	Pager     paginator.Model
	Query     string
}

type APIRouteState struct {
	Namespace string
	Repo      string
	Entries   []APIEntry
	Selected  int
	List      list.Model
	Pager     paginator.Model
	Query     string
}

type RepoExplorerRouteState struct {
	Namespace      string
	Repo           string
	Endpoints      []EndpointEntry
	Selected       int
	List           list.Model
	Pager          paginator.Model
	Query          string
	Detail         DetailState
	OperationCache map[EndpointIdentity]OperationDetail
	SpecCache      map[SpecIdentity]SpecDetail
}

func (state RepoExplorerRouteState) SelectedEndpoint() (EndpointEntry, bool) {
	if state.Selected < 0 || state.Selected >= len(state.Endpoints) {
		return EndpointEntry{}, false
	}
	return state.Endpoints[state.Selected], true
}

type loadDomain string

const (
	loadDomainNamespaceCount  loadDomain = "namespace_count"
	loadDomainRepoCount       loadDomain = "repo_count"
	loadDomainAPICount        loadDomain = "api_count"
	loadDomainOperationCount  loadDomain = "operation_count"
	loadDomainNamespaces      loadDomain = "namespaces"
	loadDomainRepoCatalog     loadDomain = "repo_catalog"
	loadDomainAPICatalog      loadDomain = "api_catalog"
	loadDomainOperationList   loadDomain = "operation_list"
	loadDomainOperationDetail loadDomain = "operation_detail"
	loadDomainSpecDetail      loadDomain = "spec_detail"
)

type asyncLoadState struct {
	ActiveToken RequestToken
	Loading     bool
	LastError   error
}

type AsyncState struct {
	nextToken       RequestToken
	NamespaceCount  asyncLoadState
	RepoCount       asyncLoadState
	APICount        asyncLoadState
	OperationCount  asyncLoadState
	Namespaces      asyncLoadState
	RepoCatalog     asyncLoadState
	APICatalog      asyncLoadState
	OperationList   asyncLoadState
	OperationDetail asyncLoadState
	SpecDetail      asyncLoadState
}
