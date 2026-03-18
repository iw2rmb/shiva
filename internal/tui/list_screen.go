package tui

import (
	"fmt"
	"sort"
	"strings"

	"charm.land/bubbles/v2/list"
)

const (
	defaultListWidth  = 80
	defaultListHeight = 20
)

func newNamespaceList() list.Model {
	delegate := list.NewDefaultDelegate()
	delegate.SetSpacing(0)
	model := list.New(nil, delegate, defaultListWidth, defaultListHeight)
	configureList(&model, "Namespaces", "namespace", "namespaces")
	return model
}

func newRepoList() list.Model {
	delegate := list.NewDefaultDelegate()
	delegate.SetSpacing(0)
	model := list.New(nil, delegate, defaultListWidth, defaultListHeight)
	configureList(&model, "Repositories", "repo", "repos")
	return model
}

func newEndpointList() list.Model {
	delegate := list.NewDefaultDelegate()
	delegate.SetSpacing(0)
	model := list.New(nil, delegate, defaultListWidth, defaultListHeight)
	configureList(&model, "Endpoints", "endpoint", "endpoints")
	return model
}

func configureList(model *list.Model, title string, singular string, plural string) {
	model.Title = title
	model.SetShowFilter(false)
	model.SetShowHelp(false)
	model.SetShowPagination(false)
	model.SetShowStatusBar(false)
	model.SetFilteringEnabled(false)
	model.SetStatusBarItemName(singular, plural)
	model.DisableQuitKeybindings()
}

type namespaceListItem struct {
	title       string
	description string
	filter      string
}

func (item namespaceListItem) FilterValue() string { return item.filter }
func (item namespaceListItem) Title() string       { return item.title }
func (item namespaceListItem) Description() string { return item.description }

type repoListItem struct {
	title       string
	description string
	filter      string
}

func (item repoListItem) FilterValue() string { return item.filter }
func (item repoListItem) Title() string       { return item.title }
func (item repoListItem) Description() string { return item.description }

type endpointListItem struct {
	title       string
	description string
	filter      string
}

func (item endpointListItem) FilterValue() string { return item.filter }
func (item endpointListItem) Title() string       { return item.title }
func (item endpointListItem) Description() string { return item.description }

func namespaceEntriesFromRepos(rows []RepoEntry) []NamespaceEntry {
	if len(rows) == 0 {
		return nil
	}

	summaries := make(map[string]NamespaceEntry)
	for _, row := range rows {
		entry := summaries[row.Namespace]
		entry.Namespace = row.Namespace
		entry.RepoCount++
		if entry.RepoCount == 1 {
			entry.AllPending = repoRowIsPending(row)
		} else {
			entry.AllPending = entry.AllPending && repoRowIsPending(row)
		}
		summaries[row.Namespace] = entry
	}

	entries := make([]NamespaceEntry, 0, len(summaries))
	for _, entry := range summaries {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Namespace < entries[j].Namespace
	})
	return entries
}

func repoEntriesByNamespace(rows []RepoEntry, namespace string) []RepoEntry {
	filtered := make([]RepoEntry, 0, len(rows))
	for _, row := range rows {
		if row.Namespace != namespace {
			continue
		}
		filtered = append(filtered, row)
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Repo < filtered[j].Repo
	})
	return filtered
}

func repoRowIsPending(row RepoEntry) bool {
	if row.Row.HeadRevision == nil {
		return false
	}
	switch strings.TrimSpace(strings.ToLower(row.Row.HeadRevision.Status)) {
	case "pending", "processing":
		return true
	default:
		return false
	}
}

func namespaceItems(entries []NamespaceEntry) []list.Item {
	items := make([]list.Item, 0, len(entries))
	for _, entry := range entries {
		description := fmt.Sprintf("%d repos", entry.RepoCount)
		if entry.RepoCount == 1 {
			description = "1 repo"
		}
		if entry.AllPending {
			description += ", all pending"
		}
		items = append(items, namespaceListItem{
			title:       entry.Namespace,
			description: description,
			filter:      entry.Namespace,
		})
	}
	return items
}

func repoItems(entries []RepoEntry) []list.Item {
	items := make([]list.Item, 0, len(entries))
	for _, entry := range entries {
		description := "catalog available"
		if repoRowIsPending(entry) {
			description = strings.TrimSpace(strings.ToLower(entry.Row.HeadRevision.Status))
		}
		items = append(items, repoListItem{
			title:       entry.Repo,
			description: description,
			filter:      entry.Namespace + "/" + entry.Repo,
		})
	}
	return items
}

func sortedEndpointEntries(entries []EndpointEntry) []EndpointEntry {
	if len(entries) == 0 {
		return nil
	}

	sorted := append([]EndpointEntry(nil), entries...)
	sort.SliceStable(sorted, func(i, j int) bool {
		left := sorted[i].Identity
		right := sorted[j].Identity
		if left.Path != right.Path {
			return left.Path < right.Path
		}
		if left.Method != right.Method {
			return left.Method < right.Method
		}
		if left.OperationID != right.OperationID {
			return left.OperationID < right.OperationID
		}
		if left.API != right.API {
			return left.API < right.API
		}
		return left.Namespace+"/"+left.Repo < right.Namespace+"/"+right.Repo
	})
	return sorted
}

func endpointItems(entries []EndpointEntry) []list.Item {
	items := make([]list.Item, 0, len(entries))
	multiAPI := hasMultipleAPIs(entries)
	for _, entry := range entries {
		title := strings.ToUpper(strings.TrimSpace(entry.Identity.Method)) + " " + strings.TrimSpace(entry.Identity.Path)
		if strings.TrimSpace(title) == "" {
			title = "unknown endpoint"
		}

		descriptionParts := make([]string, 0, 2)
		if entry.Identity.OperationID != "" {
			descriptionParts = append(descriptionParts, "#"+entry.Identity.OperationID)
		}
		if multiAPI {
			descriptionParts = append(descriptionParts, entry.Identity.API)
		}
		description := "endpoint"
		if len(descriptionParts) > 0 {
			description = strings.Join(descriptionParts, "  ")
		}

		items = append(items, endpointListItem{
			title:       title,
			description: description,
			filter:      entry.Identity.Method + " " + entry.Identity.Path + " " + entry.Identity.OperationID + " " + entry.Identity.API,
		})
	}
	return items
}

func hasMultipleAPIs(entries []EndpointEntry) bool {
	if len(entries) < 2 {
		return false
	}

	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		seen[entry.Identity.API] = struct{}{}
		if len(seen) > 1 {
			return true
		}
	}
	return false
}

func listSize(width int, height int) (int, int) {
	if width <= 0 {
		width = defaultListWidth
	}
	if height <= 0 {
		height = defaultListHeight
	}
	if width < 20 {
		width = 20
	}
	if height < 8 {
		height = 8
	}
	return width, height
}

func endpointListSize(width int, height int) (int, int) {
	width, height = listSize(width, height)
	if width >= 72 {
		width = (width - 5) / 2
	}
	if height > 10 {
		height -= 8
	}
	if height < 8 {
		height = 8
	}
	return width, height
}
