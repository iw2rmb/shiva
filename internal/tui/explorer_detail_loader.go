package tui

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/iw2rmb/shiva/internal/cli/request"
)

func batchCmds(cmds ...tea.Cmd) tea.Cmd {
	batched := make([]tea.Cmd, 0, len(cmds))
	for _, cmd := range cmds {
		if cmd == nil {
			continue
		}
		batched = append(batched, cmd)
	}
	switch len(batched) {
	case 0:
		return nil
	case 1:
		return batched[0]
	default:
		return tea.Batch(batched...)
	}
}

func (model *rootModel) clearExplorerDetailState() {
	model.explorer.Detail.Operation = nil
	model.explorer.Detail.Spec = nil
	model.invalidateLoadToken(loadDomainOperationDetail)
	model.invalidateLoadToken(loadDomainSpecDetail)
}

func (model *rootModel) invalidateLoadToken(domain loadDomain) {
	token := model.beginLoad(domain)
	model.finishLoad(domain, token, nil)
}

func (model *rootModel) loadExplorerDetailForSelection() tea.Cmd {
	return model.loadSelectedOperationDetail()
}

func (model *rootModel) loadSelectedOperationDetail() tea.Cmd {
	selected, ok := model.explorer.SelectedEndpoint()
	if !ok {
		return nil
	}
	token := model.beginOperationDetailLoad()
	if cached, ok := model.explorer.OperationCache[selected.Identity]; ok {
		detail := cached
		model.explorer.Detail.Operation = &detail
		model.finishLoad(loadDomainOperationDetail, token, nil)
		model.refreshExplorerDetailViewport()
		return nil
	}

	selector := request.Envelope{
		Namespace: selected.Identity.Namespace,
		Repo:      selected.Identity.Repo,
		API:       selected.Identity.API,
	}
	if strings.TrimSpace(selected.Identity.OperationID) != "" {
		selector.OperationID = selected.Identity.OperationID
	} else {
		selector.Method = selected.Identity.Method
		selector.Path = selected.Identity.Path
	}

	return loadOperationDetailCmd(
		context.Background(),
		model.service,
		selected.Identity,
		selector,
		model.options,
		token,
	)
}

func selectedAPIIdentity(endpoint EndpointIdentity) SpecIdentity {
	return SpecIdentity{
		Namespace: endpoint.Namespace,
		Repo:      endpoint.Repo,
		API:       endpoint.API,
	}
}
