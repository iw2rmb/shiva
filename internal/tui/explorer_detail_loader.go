package tui

import (
	"context"
	"encoding/json"

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
	operationCmd := model.loadSelectedOperationDetail()
	specCmd := model.loadSelectedSpecDetailIfNeeded()
	return batchCmds(operationCmd, specCmd)
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
		return nil
	}

	return loadOperationDetailCmd(
		context.Background(),
		model.service,
		request.Envelope{
			Namespace:   selected.Identity.Namespace,
			Repo:        selected.Identity.Repo,
			API:         selected.Identity.API,
			OperationID: selected.Identity.OperationID,
			Method:      selected.Identity.Method,
			Path:        selected.Identity.Path,
		},
		model.options,
		token,
	)
}

func (model *rootModel) loadSelectedSpecDetailIfNeeded() tea.Cmd {
	selected, ok := model.explorer.SelectedEndpoint()
	if !ok || !model.shouldLoadSelectedSpecDetail() {
		return nil
	}

	token := model.beginSpecDetailLoad()
	specIdentity := selectedSpecIdentity(selected.Identity)
	if cached, ok := model.explorer.SpecCache[specIdentity]; ok {
		detail := cached
		model.explorer.Detail.Spec = &detail
		model.finishLoad(loadDomainSpecDetail, token, nil)
		return nil
	}

	return loadSpecDetailCmd(
		context.Background(),
		model.service,
		request.Envelope{
			Namespace: specIdentity.Namespace,
			Repo:      specIdentity.Repo,
			API:       specIdentity.API,
		},
		model.options,
		token,
	)
}

func (model *rootModel) shouldLoadSelectedSpecDetail() bool {
	if model.explorer.Detail.ActiveTab != DetailTabServers {
		return false
	}
	selected, ok := model.explorer.SelectedEndpoint()
	if !ok || model.explorer.Detail.Operation == nil {
		return false
	}
	if model.explorer.Detail.Operation.Endpoint != selected.Identity {
		return false
	}
	return operationRequiresSpecServers(model.explorer.Detail.Operation.Body)
}

func selectedSpecIdentity(endpoint EndpointIdentity) SpecIdentity {
	return SpecIdentity{
		Namespace: endpoint.Namespace,
		Repo:      endpoint.Repo,
		API:       endpoint.API,
	}
}

func operationRequiresSpecServers(body json.RawMessage) bool {
	var operation map[string]json.RawMessage
	if err := json.Unmarshal(body, &operation); err != nil {
		return true
	}
	serversRaw, ok := operation["servers"]
	if !ok {
		return true
	}

	var servers []json.RawMessage
	if err := json.Unmarshal(serversRaw, &servers); err != nil {
		return true
	}
	return len(servers) == 0
}
