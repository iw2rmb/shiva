package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/iw2rmb/shiva/internal/cli/request"
)

func (model *rootModel) clearAPIDetailState() {
	model.apiList.Detail.Spec = nil
	model.apiList.Detail.Issues = nil
	model.invalidateLoadToken(loadDomainAPISpecDetail)
	model.invalidateLoadToken(loadDomainAPIIssues)
}

func (model *rootModel) selectedAPIEntry() (APIEntry, bool) {
	if model.apiList.Selected < 0 || model.apiList.Selected >= len(model.apiList.Entries) {
		return APIEntry{}, false
	}
	return model.apiList.Entries[model.apiList.Selected], true
}

func (model *rootModel) selectedAPIIdentity() (SpecIdentity, bool) {
	selected, ok := model.selectedAPIEntry()
	if !ok {
		return SpecIdentity{}, false
	}
	return SpecIdentity{Namespace: selected.Namespace, Repo: selected.Repo, API: selected.API}, true
}

func (model *rootModel) ensureAPIDetailLoadForTab() tea.Cmd {
	identity, ok := model.selectedAPIIdentity()
	if !ok {
		return nil
	}
	selector := request.Envelope{
		Namespace: identity.Namespace,
		Repo:      identity.Repo,
		API:       identity.API,
	}

	switch model.apiList.Detail.ActiveTab {
	case APIDetailTabData:
		if model.async.APISpecDetail.Loading {
			return nil
		}
		token := model.beginAPISpecDetailLoad()
		if cached, ok := model.apiList.SpecCache[identity]; ok {
			detail := cached
			model.apiList.Detail.Spec = &detail
			model.finishLoad(loadDomainAPISpecDetail, token, nil)
			model.refreshAPIDetailViewport()
			return nil
		}
		return loadAPISpecDetailCmd(context.Background(), model.service, selector, model.options, token)
	case APIDetailTabIssues:
		if model.async.APIIssues.Loading {
			return nil
		}
		token := model.beginAPIIssuesLoad()
		if cached, ok := model.apiList.IssueCache[identity]; ok {
			detail := cached
			model.apiList.Detail.Issues = &detail
			model.finishLoad(loadDomainAPIIssues, token, nil)
			model.refreshAPIDetailViewport()
			return nil
		}
		return loadAPIIssuesCmd(context.Background(), model.service, selector, model.options, token)
	default:
		return nil
	}
}

func (model *rootModel) switchAPIDetailTab(delta int) tea.Cmd {
	tabs := []APIDetailTab{APIDetailTabData, APIDetailTabIssues}
	index := 0
	for i, tab := range tabs {
		if tab == model.apiList.Detail.ActiveTab {
			index = i
			break
		}
	}
	index = (index + delta + len(tabs)) % len(tabs)
	model.apiList.Detail.ActiveTab = tabs[index]
	model.refreshAPIDetailViewport()
	return model.ensureAPIDetailLoadForTab()
}
