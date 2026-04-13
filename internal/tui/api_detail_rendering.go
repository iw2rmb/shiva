package tui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/iw2rmb/shiva/internal/textutil"
)

type apiSpecSummary struct {
	OpenAPIVersion string
	Description    string
	Servers        []apiSpecServerSummary
}

type apiSpecServerSummary struct {
	Description string
	URL         string
}

type apiSpecDocument struct {
	OpenAPI string `json:"openapi"`
	Info    struct {
		Description string `json:"description"`
	} `json:"info"`
	Servers []struct {
		Description string `json:"description"`
		URL         string `json:"url"`
	} `json:"servers"`
}

func (model *rootModel) refreshAPIDetailViewport() {
	markdownBody := model.apiDetailMarkdown()
	width := model.apiList.Detail.Viewport.Width()
	rendered := model.markdown.Render(markdownBody, width)
	model.apiList.Detail.Viewport.SetContent(rendered)
}

func (model *rootModel) apiDetailMarkdown() string {
	selected, ok := model.selectedAPIEntry()
	if !ok {
		return strings.Join([]string{
			"## Details",
			"",
			"Select an API to show details.",
		}, "\n")
	}
	identity := SpecIdentity{Namespace: selected.Namespace, Repo: selected.Repo, API: selected.API}

	switch model.apiList.Detail.ActiveTab {
	case APIDetailTabData:
		if model.apiList.Detail.Spec == nil {
			if model.async.APISpecDetail.LastError != nil {
				return renderDetailLoadError("Data", model.async.APISpecDetail.LastError)
			}
			if model.async.APISpecDetail.Loading {
				return "Loading API data..."
			}
			return "No API data available for the selected API."
		}
		return buildAPIDataMarkdown(selected, model.apiList.Detail.Spec)
	case APIDetailTabIssues:
		if model.apiList.Detail.Issues == nil {
			if model.async.APIIssues.LastError != nil {
				return renderDetailLoadError("Issues", model.async.APIIssues.LastError)
			}
			if model.async.APIIssues.Loading {
				return strings.Join([]string{"## Issues", "", "Loading vacuum issues..."}, "\n")
			}
			return strings.Join([]string{"## Issues", "", "No vacuum issues available for the selected API."}, "\n")
		}
		if model.apiList.Detail.Issues.API != identity {
			return strings.Join([]string{"## Issues", "", "No vacuum issues available for the selected API."}, "\n")
		}
		return buildAPIIssuesMarkdown(selected, model.apiList.Detail.Issues)
	default:
		return "## Details"
	}
}

func buildAPIDataMarkdown(selected APIEntry, detail *SpecDetail) string {
	summary := parseAPISpecSummary(detail.Body)
	rows := []string{
		"Status: " + valueOrDash(selected.Row.Status),
		"Ingest: " + formatAPIIngest(
			selected.Row.IngestEventBranch,
			selected.Row.IngestEventSHA,
			selected.Row.IngestEventProcessedAt,
		),
		"Revision: " + int64OrDash(selected.Row.APISpecRevisionID),
		"",
		valueOrDash(summary.Description),
		"",
		"Servers:",
	}
	if len(summary.Servers) == 0 {
		rows = append(rows, "- -")
	} else {
		for _, server := range summary.Servers {
			rows = append(rows, fmt.Sprintf("- %s: %s", valueOrDash(server.Description), valueOrDash(server.URL)))
		}
	}
	rows = append(rows, "", "OpenAPI v"+valueOrDash(summary.OpenAPIVersion))
	return strings.Join(rows, "\n")
}

func buildAPIIssuesMarkdown(selected APIEntry, detail *APIIssuesDetail) string {
	rows := []string{
		"## Issues",
		"",
		fmt.Sprintf("`Repo:` `%s/%s`", valueOrDash(selected.Namespace), valueOrDash(selected.Repo)),
		fmt.Sprintf("`API:` `%s`", valueOrDash(selected.API)),
		fmt.Sprintf("`Vacuum Status:` `%s`", valueOrDash(detail.VacuumStatus)),
		fmt.Sprintf("`Vacuum Error:` %s", valueOrDash(detail.VacuumError)),
		fmt.Sprintf("`Validated At:` `%s`", timeOrDash(detail.VacuumValidatedAt)),
	}
	if len(detail.Issues) == 0 {
		rows = append(rows, "", "No vacuum issues.")
		return strings.Join(rows, "\n")
	}
	rows = append(rows, "", "## Vacuum Errors")
	for _, issue := range detail.Issues {
		line := "- " + valueOrDash(issue.RuleID)
		if strings.TrimSpace(issue.Message) != "" {
			line += ": " + strings.TrimSpace(issue.Message)
		}
		if strings.TrimSpace(issue.JSONPath) != "" {
			line += " (`" + strings.TrimSpace(issue.JSONPath) + "`)"
		}
		rows = append(rows, line)
	}
	return strings.Join(rows, "\n")
}

func parseAPISpecSummary(raw json.RawMessage) apiSpecSummary {
	var document apiSpecDocument
	if len(raw) == 0 {
		return apiSpecSummary{}
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		return apiSpecSummary{}
	}
	summary := apiSpecSummary{
		OpenAPIVersion: strings.TrimSpace(document.OpenAPI),
		Description:    strings.TrimSpace(document.Info.Description),
	}
	seen := make(map[string]struct{}, len(document.Servers))
	for _, server := range document.Servers {
		description := strings.TrimSpace(server.Description)
		url := strings.TrimSpace(server.URL)
		if description == "" && url == "" {
			continue
		}
		key := description + "\x00" + url
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		summary.Servers = append(summary.Servers, apiSpecServerSummary{
			Description: description,
			URL:         url,
		})
	}
	sort.Slice(summary.Servers, func(i int, j int) bool {
		left := summary.Servers[i]
		right := summary.Servers[j]
		if left.Description != right.Description {
			return left.Description < right.Description
		}
		return left.URL < right.URL
	})
	return summary
}

func valueOrDash(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "-"
	}
	return trimmed
}

func int64OrDash(value int64) string {
	if value < 1 {
		return "-"
	}
	return fmt.Sprintf("%d", value)
}

func formatAPIIngest(branch string, sha string, processedAt *time.Time) string {
	return fmt.Sprintf(
		"%s (%s) @ %s",
		valueOrDash(branch),
		valueOrDash(textutil.ShortSHA(sha)),
		ingestProcessedAtOrDash(processedAt),
	)
}

func ingestProcessedAtOrDash(value *time.Time) string {
	if value == nil {
		return "-"
	}
	return value.UTC().Format("02-01-06 15:04:05")
}

func timeOrDash(value *time.Time) string {
	if value == nil {
		return "-"
	}
	return value.UTC().Format(time.RFC3339)
}
