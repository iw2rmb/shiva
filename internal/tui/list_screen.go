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
