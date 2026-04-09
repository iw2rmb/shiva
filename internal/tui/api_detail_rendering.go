package tui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

type apiSpecSummary struct {
	OpenAPIVersion string
	Description    string
	Servers        []string
}

type apiSpecDocument struct {
	OpenAPI string `json:"openapi"`
	Info    struct {
		Description string `json:"description"`
	} `json:"info"`
	Servers []struct {
		URL string `json:"url"`
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
		"## Data",
		"",
		fmt.Sprintf("`Repo:` `%s/%s`", valueOrDash(selected.Namespace), valueOrDash(selected.Repo)),
		fmt.Sprintf("`API:` `%s`", valueOrDash(selected.API)),
		fmt.Sprintf("`Title:` %s", valueOrDash(selected.Title)),
		fmt.Sprintf("`Description:` %s", valueOrDash(summary.Description)),
		fmt.Sprintf("`OpenAPI:` `%s`", valueOrDash(summary.OpenAPIVersion)),
		fmt.Sprintf("`Status:` `%s`", valueOrDash(strings.TrimSpace(selected.Row.Status))),
		fmt.Sprintf("`Has Snapshot:` `%t`", selected.Row.HasSnapshot),
		fmt.Sprintf("`API Spec Revision:` `%s`", int64OrDash(selected.Row.APISpecRevisionID)),
		fmt.Sprintf("`Ingest SHA:` `%s`", valueOrDash(strings.TrimSpace(selected.Row.IngestEventSHA))),
		fmt.Sprintf("`Ingest Branch:` `%s`", valueOrDash(strings.TrimSpace(selected.Row.IngestEventBranch))),
		fmt.Sprintf("`Operation Count:` `%d`", selected.Row.OperationCount),
		fmt.Sprintf("`Spec ETag:` `%s`", valueOrDash(strings.TrimSpace(selected.Row.SpecETag))),
		fmt.Sprintf("`Spec Size:` `%s`", sizeOrDash(selected.Row.SpecSizeBytes)),
		"",
		"## Servers",
	}
	if len(summary.Servers) == 0 {
		rows = append(rows, "- -")
	} else {
		for _, server := range summary.Servers {
			rows = append(rows, "- `"+server+"`")
		}
	}
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
		url := strings.TrimSpace(server.URL)
		if url == "" {
			continue
		}
		if _, ok := seen[url]; ok {
			continue
		}
		seen[url] = struct{}{}
		summary.Servers = append(summary.Servers, url)
	}
	sort.Strings(summary.Servers)
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

func sizeOrDash(value int64) string {
	if value < 1 {
		return "-"
	}
	return fmt.Sprintf("%d bytes", value)
}

func timeOrDash(value *time.Time) string {
	if value == nil {
		return "-"
	}
	return value.UTC().Format(time.RFC3339)
}
