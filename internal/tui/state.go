package tui

import (
	"encoding/json"

	"charm.land/bubbles/v2/list"
	clioutput "github.com/iw2rmb/shiva/internal/cli/output"
)

type DetailTab string

const (
	DetailTabEndpoints DetailTab = "endpoints"
	DetailTabServers   DetailTab = "servers"
	DetailTabErrors    DetailTab = "errors"
)

type NamespaceEntry struct {
	Namespace  string
	RepoCount  int
	AllPending bool
}

type RepoEntry struct {
	Namespace string
	Repo      string
	Row       clioutput.RepoRow
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
}

type NamespaceRouteState struct {
	Entries  []NamespaceEntry
	Selected int
	List     list.Model
}

type RepoRouteState struct {
	Namespace string
	Entries   []RepoEntry
	Selected  int
	List      list.Model
}

type RepoExplorerRouteState struct {
	Namespace string
	Repo      string
	Endpoints []EndpointEntry
	Selected  int
	Detail    DetailState
}

func (state RepoExplorerRouteState) SelectedEndpoint() (EndpointEntry, bool) {
	if state.Selected < 0 || state.Selected >= len(state.Endpoints) {
		return EndpointEntry{}, false
	}
	return state.Endpoints[state.Selected], true
}

type loadDomain string

const (
	loadDomainRepoCatalog     loadDomain = "repo_catalog"
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
	RepoCatalog     asyncLoadState
	OperationList   asyncLoadState
	OperationDetail asyncLoadState
	SpecDetail      asyncLoadState
}
