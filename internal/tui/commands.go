package tui

import (
	"context"
	"encoding/json"
	"fmt"

	tea "charm.land/bubbletea/v2"
	clioutput "github.com/iw2rmb/shiva/internal/cli/output"
	"github.com/iw2rmb/shiva/internal/cli/request"
)

func loadRepoCatalogCmd(
	ctx context.Context,
	service BrowserService,
	options RequestOptions,
	token RequestToken,
) tea.Cmd {
	return func() tea.Msg {
		return loadRepoCatalogMsg(ctx, service, options, token)
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

func loadNamespaceCatalogCmd(
	ctx context.Context,
	service BrowserService,
	options RequestOptions,
	token RequestToken,
) tea.Cmd {
	return func() tea.Msg {
		return loadNamespaceCatalogMsg(ctx, service, options, token)
	}
}

func loadNamespaceCatalogMsg(
	ctx context.Context,
	service BrowserService,
	options RequestOptions,
	token RequestToken,
) tea.Msg {
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

	return namespaceCatalogLoadedMsg{Token: token, Rows: entries}
}

func loadRepoCatalogMsg(
	ctx context.Context,
	service BrowserService,
	options RequestOptions,
	token RequestToken,
) tea.Msg {
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

	return repoCatalogLoadedMsg{Token: token, Rows: entries}
}

func loadOperationListCmd(
	ctx context.Context,
	service BrowserService,
	selector request.Envelope,
	options RequestOptions,
	token RequestToken,
) tea.Cmd {
	return func() tea.Msg {
		return loadOperationListMsg(ctx, service, selector, options, token)
	}
}

func loadOperationListMsg(
	ctx context.Context,
	service BrowserService,
	selector request.Envelope,
	options RequestOptions,
	token RequestToken,
) tea.Msg {
	body, err := service.ListOperations(ctx, selector, options, clioutput.ListFormatJSON)
	if err != nil {
		return loadFailedMsg{Domain: loadDomainOperationList, Token: token, Err: err}
	}

	var rows []clioutput.OperationRow
	if err := json.Unmarshal(body, &rows); err != nil {
		return loadFailedMsg{
			Domain: loadDomainOperationList,
			Token:  token,
			Err:    fmt.Errorf("decode operation list: %w", err),
		}
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

	return operationListLoadedMsg{Token: token, Entries: entries}
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
