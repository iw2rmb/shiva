package tui

type RequestToken uint64

type repoCatalogLoadedMsg struct {
	Token  RequestToken
	Limit  int32
	Offset int32
	Rows   []RepoEntry
}

type namespaceCatalogLoadedMsg struct {
	Token  RequestToken
	Limit  int32
	Offset int32
	Rows   []NamespaceEntry
}

type apiCatalogLoadedMsg struct {
	Token     RequestToken
	Namespace string
	Repo      string
	Rows      []APIEntry
}

type namespaceCountLoadedMsg struct {
	Token RequestToken
	Count CatalogCount
}

type repoCountLoadedMsg struct {
	Token     RequestToken
	Namespace string
	Count     CatalogCount
}

type apiCountLoadedMsg struct {
	Token     RequestToken
	Namespace string
	Repo      string
	Count     CatalogCount
}

type operationCountLoadedMsg struct {
	Token     RequestToken
	Namespace string
	Repo      string
	Count     CatalogCount
}

type operationListLoadedMsg struct {
	Token   RequestToken
	Limit   int32
	Offset  int32
	Entries []EndpointEntry
}

type operationDetailLoadedMsg struct {
	Token  RequestToken
	Detail OperationDetail
}

type specDetailLoadedMsg struct {
	Token  RequestToken
	Detail SpecDetail
}

type resizeMsg struct {
	Width  int
	Height int
}

type loadFailedMsg struct {
	Domain loadDomain
	Token  RequestToken
	Err    error
}
