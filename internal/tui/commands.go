package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	clioutput "github.com/iw2rmb/shiva/internal/cli/output"
	"github.com/iw2rmb/shiva/internal/cli/request"
)

func loadRepoCatalogCmd(
	ctx context.Context,
	service BrowserService,
	options RequestOptions,
	offset int32,
	token RequestToken,
) tea.Cmd {
	return func() tea.Msg {
		return loadRepoCatalogMsg(ctx, service, options, offset, token)
	}
}

func loadNamespaceCountCmd(
	ctx context.Context,
	service BrowserService,
	options RequestOptions,
	token RequestToken,
) tea.Cmd {
	return func() tea.Msg {
		return loadNamespaceCountMsg(ctx, service, options, token)
	}
}

func loadNamespaceCountMsg(
	ctx context.Context,
	service BrowserService,
	options RequestOptions,
	token RequestToken,
) tea.Msg {
	count, err := service.CountNamespaces(ctx, options)
	if err != nil {
		return loadFailedMsg{Domain: loadDomainNamespaceCount, Token: token, Err: err}
	}
	return namespaceCountLoadedMsg{Token: token, Count: count}
}

func loadRepoCountCmd(
	ctx context.Context,
	service BrowserService,
	namespace string,
	options RequestOptions,
	token RequestToken,
) tea.Cmd {
	return func() tea.Msg {
		count, err := service.CountRepos(ctx, namespace, options)
		if err != nil {
			return loadFailedMsg{Domain: loadDomainRepoCount, Token: token, Err: err}
		}
		return repoCountLoadedMsg{
			Token:     token,
			Namespace: namespace,
			Count:     count,
		}
	}
}

func loadOperationCountCmd(
	ctx context.Context,
	service BrowserService,
	selector request.Envelope,
	options RequestOptions,
	token RequestToken,
) tea.Cmd {
	return func() tea.Msg {
		count, err := service.CountOperations(ctx, selector, options)
		if err != nil {
			return loadFailedMsg{Domain: loadDomainOperationCount, Token: token, Err: err}
		}
		return operationCountLoadedMsg{
			Token:     token,
			Namespace: selector.Namespace,
			Repo:      selector.Repo,
			Count:     count,
		}
	}
}

func loadAPICountCmd(
	ctx context.Context,
	service BrowserService,
	selector request.Envelope,
	options RequestOptions,
	token RequestToken,
) tea.Cmd {
	return func() tea.Msg {
		count, err := service.CountAPIs(ctx, selector, options)
		if err != nil {
			return loadFailedMsg{Domain: loadDomainAPICount, Token: token, Err: err}
		}
		return apiCountLoadedMsg{
			Token:     token,
			Namespace: selector.Namespace,
			Repo:      selector.Repo,
			Count:     count,
		}
	}
}

func loadNamespaceCatalogCmd(
	ctx context.Context,
	service BrowserService,
	options RequestOptions,
	offset int32,
	token RequestToken,
) tea.Cmd {
	return func() tea.Msg {
		return loadNamespaceCatalogMsg(ctx, service, options, offset, token)
	}
}

func loadNamespaceCatalogMsg(
	ctx context.Context,
	service BrowserService,
	options RequestOptions,
	offset int32,
	token RequestToken,
) tea.Msg {
	options.Offset = offset
	body, err := service.ListNamespaces(ctx, options, clioutput.ListFormatJSON)
	if err != nil {
		return loadFailedMsg{Domain: loadDomainNamespaces, Token: token, Err: err}
	}

	var rows []clioutput.NamespaceRow
	if err := json.Unmarshal(body, &rows); err != nil {
		return loadFailedMsg{
			Domain: loadDomainNamespaces,
			Token:  token,
			Err:    fmt.Errorf("decode namespace catalog: %w", err),
		}
	}

	entries := make([]NamespaceEntry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, NamespaceEntry{
			Namespace:  row.Namespace,
			RepoCount:  int(row.RepoCount),
			AllPending: row.AllPending,
		})
	}

	return namespaceCatalogLoadedMsg{Token: token, Limit: options.Limit, Offset: offset, Rows: entries}
}

func loadRepoCatalogMsg(
	ctx context.Context,
	service BrowserService,
	options RequestOptions,
	offset int32,
	token RequestToken,
) tea.Msg {
	options.Offset = offset
	body, err := service.ListRepos(ctx, options, clioutput.ListFormatJSON)
	if err != nil {
		return loadFailedMsg{Domain: loadDomainRepoCatalog, Token: token, Err: err}
	}

	var rows []clioutput.RepoRow
	if err := json.Unmarshal(body, &rows); err != nil {
		return loadFailedMsg{
			Domain: loadDomainRepoCatalog,
			Token:  token,
			Err:    fmt.Errorf("decode repo catalog: %w", err),
		}
	}

	entries := make([]RepoEntry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, RepoEntry{
			Namespace: row.Namespace,
			Repo:      row.Repo,
			Row:       row,
		})
	}

	return repoCatalogLoadedMsg{Token: token, Limit: options.Limit, Offset: offset, Rows: entries}
}

func loadOperationListCmd(
	ctx context.Context,
	service BrowserService,
	selector request.Envelope,
	options RequestOptions,
	offset int32,
	token RequestToken,
) tea.Cmd {
	return func() tea.Msg {
		return loadOperationListMsg(ctx, service, selector, options, offset, token)
	}
}

func loadAPICatalogCmd(
	ctx context.Context,
	service BrowserService,
	selector request.Envelope,
	options RequestOptions,
	token RequestToken,
) tea.Cmd {
	return func() tea.Msg {
		return loadAPICatalogMsg(ctx, service, selector, options, token)
	}
}

func loadAPICatalogMsg(
	ctx context.Context,
	service BrowserService,
	selector request.Envelope,
	options RequestOptions,
	token RequestToken,
) tea.Msg {
	body, err := service.ListAPIs(ctx, selector, options, clioutput.ListFormatJSON)
	if err != nil {
		return loadFailedMsg{Domain: loadDomainAPICatalog, Token: token, Err: err}
	}

	var rows []clioutput.APIRow
	if err := json.Unmarshal(body, &rows); err != nil {
		return loadFailedMsg{
			Domain: loadDomainAPICatalog,
			Token:  token,
			Err:    fmt.Errorf("decode api catalog: %w", err),
		}
	}

	entries := make([]APIEntry, 0, len(rows))
	for _, row := range rows {
		namespace := strings.TrimSpace(row.Namespace)
		if namespace == "" {
			namespace = selector.Namespace
		}
		repo := strings.TrimSpace(row.Repo)
		if repo == "" {
			repo = selector.Repo
		}
		title := strings.TrimSpace(row.Title)
		if title == "" {
			title = strings.TrimSpace(row.DisplayName)
		}
		if title == "" {
			title = strings.TrimSpace(row.API)
		}
		entries = append(entries, APIEntry{
			Namespace: namespace,
			Repo:      repo,
			Title:     title,
			API:       strings.TrimSpace(row.API),
			Row:       row,
		})
	}

	return apiCatalogLoadedMsg{
		Token:     token,
		Namespace: selector.Namespace,
		Repo:      selector.Repo,
		Rows:      entries,
	}
}

func loadOperationListMsg(
	ctx context.Context,
	service BrowserService,
	selector request.Envelope,
	options RequestOptions,
	offset int32,
	token RequestToken,
) tea.Msg {
	options.Offset = offset
	body, err := service.ListOperations(ctx, selector, options, clioutput.ListFormatJSON)
	if err != nil {
		return loadFailedMsg{Domain: loadDomainOperationList, Token: token, Err: err}
	}

	entries, err := decodeOperationEntries(body)
	if err != nil {
		return loadFailedMsg{
			Domain: loadDomainOperationList,
			Token:  token,
			Err:    err,
		}
	}

	return operationListLoadedMsg{Token: token, Limit: options.Limit, Offset: offset, Entries: entries}
}

func decodeOperationEntries(body []byte) ([]EndpointEntry, error) {
	var rows []clioutput.OperationRow
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("decode operation list: %w", err)
	}

	entries := make([]EndpointEntry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, EndpointEntry{
			Identity: EndpointIdentity{
				Namespace:   row.Namespace,
				Repo:        row.Repo,
				API:         row.API,
				OperationID: row.OperationID,
				Method:      row.Method,
				Path:        row.Path,
			},
			Row: row,
		})
	}

	return entries, nil
}

func loadOperationDetailCmd(
	ctx context.Context,
	service BrowserService,
	selector request.Envelope,
	options RequestOptions,
	token RequestToken,
) tea.Cmd {
	return func() tea.Msg {
		return loadOperationDetailMsg(ctx, service, selector, options, token)
	}
}

func loadOperationDetailMsg(
	ctx context.Context,
	service BrowserService,
	selector request.Envelope,
	options RequestOptions,
	token RequestToken,
) tea.Msg {
	body, err := service.GetOperation(ctx, selector, options)
	if err != nil {
		return loadFailedMsg{Domain: loadDomainOperationDetail, Token: token, Err: err}
	}

	return operationDetailLoadedMsg{
		Token: token,
		Detail: OperationDetail{
			Endpoint: EndpointIdentity{
				Namespace:   selector.Namespace,
				Repo:        selector.Repo,
				API:         selector.API,
				OperationID: selector.OperationID,
				Method:      selector.Method,
				Path:        selector.Path,
			},
			Body: append(json.RawMessage(nil), body...),
		},
	}
}

func loadSpecDetailCmd(
	ctx context.Context,
	service BrowserService,
	selector request.Envelope,
	options RequestOptions,
	token RequestToken,
) tea.Cmd {
	return func() tea.Msg {
		return loadSpecDetailMsg(ctx, service, selector, options, token)
	}
}

func loadSpecDetailMsg(
	ctx context.Context,
	service BrowserService,
	selector request.Envelope,
	options RequestOptions,
	token RequestToken,
) tea.Msg {
	body, err := service.GetSpec(ctx, selector, options, SpecFormatJSON)
	if err != nil {
		return loadFailedMsg{Domain: loadDomainSpecDetail, Token: token, Err: err}
	}

	return specDetailLoadedMsg{
		Token: token,
		Detail: SpecDetail{
			Namespace: selector.Namespace,
			Repo:      selector.Repo,
			API:       selector.API,
			Revision:  selector.RevisionID,
			SHA:       selector.SHA,
			Body:      append(json.RawMessage(nil), body...),
		},
	}
}
