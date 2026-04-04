package tui

type RequestToken uint64

type repoCatalogLoadedMsg struct {
	Token RequestToken
	Rows  []RepoEntry
}

type namespaceCatalogLoadedMsg struct {
	Token RequestToken
	Rows  []NamespaceEntry
}

type namespaceCountLoadedMsg struct {
	Token RequestToken
	Count int64
}

type operationListLoadedMsg struct {
	Token   RequestToken
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
