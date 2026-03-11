package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"
	"time"
)

type ListFormat string

const (
	ListFormatTable  ListFormat = "table"
	ListFormatTSV    ListFormat = "tsv"
	ListFormatJSON   ListFormat = "json"
	ListFormatNDJSON ListFormat = "ndjson"
)

type RevisionState struct {
	ID             int64      `json:"id"`
	SHA            string     `json:"sha,omitempty"`
	Status         string     `json:"status,omitempty"`
	OpenAPIChanged *bool      `json:"openapi_changed,omitempty"`
	ReceivedAt     *time.Time `json:"received_at,omitempty"`
	ProcessedAt    *time.Time `json:"processed_at,omitempty"`
}

type RepoRow struct {
	Repo               string         `json:"repo"`
	GitLabProjectID    int64          `json:"gitlab_project_id"`
	DefaultBranch      string         `json:"default_branch"`
	OpenAPIForceRescan bool           `json:"openapi_force_rescan"`
	ActiveAPICount     int64          `json:"active_api_count"`
	HeadRevision       *RevisionState `json:"head_revision,omitempty"`
	SnapshotRevision   *RevisionState `json:"snapshot_revision,omitempty"`
}

type APIRow struct {
	Repo              string `json:"repo,omitempty"`
	API               string `json:"api"`
	Status            string `json:"status"`
	DisplayName       string `json:"display_name,omitempty"`
	HasSnapshot       bool   `json:"has_snapshot"`
	APISpecRevisionID int64  `json:"api_spec_revision_id,omitempty"`
	IngestEventID     int64  `json:"ingest_event_id,omitempty"`
	IngestEventSHA    string `json:"ingest_event_sha,omitempty"`
	IngestEventBranch string `json:"ingest_event_branch,omitempty"`
	SpecETag          string `json:"spec_etag,omitempty"`
	SpecSizeBytes     int64  `json:"spec_size_bytes,omitempty"`
	OperationCount    int64  `json:"operation_count"`
}

type OperationRow struct {
	Repo              string          `json:"repo,omitempty"`
	API               string          `json:"api"`
	Status            string          `json:"status"`
	APISpecRevisionID int64           `json:"api_spec_revision_id"`
	IngestEventID     int64           `json:"ingest_event_id"`
	IngestEventSHA    string          `json:"ingest_event_sha"`
	IngestEventBranch string          `json:"ingest_event_branch"`
	Method            string          `json:"method"`
	Path              string          `json:"path"`
	OperationID       string          `json:"operation_id,omitempty"`
	Summary           string          `json:"summary,omitempty"`
	Deprecated        bool            `json:"deprecated"`
	Operation         json.RawMessage `json:"operation,omitempty"`
}

func RenderRepos(rows []RepoRow, format ListFormat) ([]byte, error) {
	switch format {
	case ListFormatTable:
		return renderRepoTable(rows), nil
	case ListFormatTSV:
		return renderRepoTSV(rows), nil
	case ListFormatJSON:
		return renderJSON(rows)
	case ListFormatNDJSON:
		return renderNDJSON(rows)
	default:
		return nil, fmt.Errorf("unsupported list format %q", format)
	}
}

func RenderAPIs(rows []APIRow, format ListFormat) ([]byte, error) {
	switch format {
	case ListFormatTable:
		return renderAPITable(rows), nil
	case ListFormatTSV:
		return renderAPITSV(rows), nil
	case ListFormatJSON:
		return renderJSON(rows)
	case ListFormatNDJSON:
		return renderNDJSON(rows)
	default:
		return nil, fmt.Errorf("unsupported list format %q", format)
	}
}

func RenderOperations(rows []OperationRow, format ListFormat) ([]byte, error) {
	switch format {
	case ListFormatTable:
		return renderOperationTable(rows), nil
	case ListFormatTSV:
		return renderOperationTSV(rows), nil
	case ListFormatJSON:
		return renderJSON(rows)
	case ListFormatNDJSON:
		return renderNDJSON(rows)
	default:
		return nil, fmt.Errorf("unsupported list format %q", format)
	}
}

func renderRepoTable(rows []RepoRow) []byte {
	buffer := &bytes.Buffer{}
	writer := tabwriter.NewWriter(buffer, 0, 0, 2, ' ', 0)
	fmt.Fprintln(writer, "REPO\tDEFAULT_BRANCH\tACTIVE_APIS\tSNAPSHOT\tHEAD")
	for _, row := range rows {
		fmt.Fprintf(
			writer,
			"%s\t%s\t%d\t%s\t%s\n",
			sanitizeCell(row.Repo),
			sanitizeCell(row.DefaultBranch),
			row.ActiveAPICount,
			sanitizeCell(renderRevisionSummary(row.SnapshotRevision)),
			sanitizeCell(renderRevisionSummary(row.HeadRevision)),
		)
	}
	_ = writer.Flush()
	return buffer.Bytes()
}

func renderRepoTSV(rows []RepoRow) []byte {
	buffer := &bytes.Buffer{}
	fmt.Fprintln(buffer, "repo\tdefault_branch\tactive_api_count\tsnapshot_revision\thead_revision")
	for _, row := range rows {
		fmt.Fprintf(
			buffer,
			"%s\t%s\t%d\t%s\t%s\n",
			sanitizeCell(row.Repo),
			sanitizeCell(row.DefaultBranch),
			row.ActiveAPICount,
			sanitizeCell(renderRevisionSummary(row.SnapshotRevision)),
			sanitizeCell(renderRevisionSummary(row.HeadRevision)),
		)
	}
	return buffer.Bytes()
}

func renderAPITable(rows []APIRow) []byte {
	buffer := &bytes.Buffer{}
	writer := tabwriter.NewWriter(buffer, 0, 0, 2, ' ', 0)
	fmt.Fprintln(writer, "API\tSTATUS\tSNAPSHOT\tOPS\tDISPLAY_NAME")
	for _, row := range rows {
		fmt.Fprintf(
			writer,
			"%s\t%s\t%t\t%d\t%s\n",
			sanitizeCell(row.API),
			sanitizeCell(row.Status),
			row.HasSnapshot,
			row.OperationCount,
			sanitizeCell(row.DisplayName),
		)
	}
	_ = writer.Flush()
	return buffer.Bytes()
}

func renderAPITSV(rows []APIRow) []byte {
	buffer := &bytes.Buffer{}
	fmt.Fprintln(buffer, "repo\tapi\tstatus\thas_snapshot\toperation_count\tdisplay_name")
	for _, row := range rows {
		fmt.Fprintf(
			buffer,
			"%s\t%s\t%s\t%t\t%d\t%s\n",
			sanitizeCell(row.Repo),
			sanitizeCell(row.API),
			sanitizeCell(row.Status),
			row.HasSnapshot,
			row.OperationCount,
			sanitizeCell(row.DisplayName),
		)
	}
	return buffer.Bytes()
}

func renderOperationTable(rows []OperationRow) []byte {
	buffer := &bytes.Buffer{}
	writer := tabwriter.NewWriter(buffer, 0, 0, 2, ' ', 0)
	fmt.Fprintln(writer, "API\tMETHOD\tPATH\tOPERATION_ID\tDEPRECATED\tSUMMARY")
	for _, row := range rows {
		fmt.Fprintf(
			writer,
			"%s\t%s\t%s\t%s\t%t\t%s\n",
			sanitizeCell(row.API),
			sanitizeCell(strings.ToUpper(row.Method)),
			sanitizeCell(row.Path),
			sanitizeCell(row.OperationID),
			row.Deprecated,
			sanitizeCell(row.Summary),
		)
	}
	_ = writer.Flush()
	return buffer.Bytes()
}

func renderOperationTSV(rows []OperationRow) []byte {
	buffer := &bytes.Buffer{}
	fmt.Fprintln(buffer, "repo\tapi\tmethod\tpath\toperation_id\tdeprecated\tsummary")
	for _, row := range rows {
		fmt.Fprintf(
			buffer,
			"%s\t%s\t%s\t%s\t%s\t%t\t%s\n",
			sanitizeCell(row.Repo),
			sanitizeCell(row.API),
			sanitizeCell(row.Method),
			sanitizeCell(row.Path),
			sanitizeCell(row.OperationID),
			row.Deprecated,
			sanitizeCell(row.Summary),
		)
	}
	return buffer.Bytes()
}

func renderJSON[T any](rows []T) ([]byte, error) {
	return json.Marshal(rows)
}

func renderNDJSON[T any](rows []T) ([]byte, error) {
	if len(rows) == 0 {
		return nil, nil
	}

	buffer := &bytes.Buffer{}
	encoder := json.NewEncoder(buffer)
	for _, row := range rows {
		if err := encoder.Encode(row); err != nil {
			return nil, fmt.Errorf("encode ndjson row: %w", err)
		}
	}
	return buffer.Bytes(), nil
}

func renderRevisionSummary(revision *RevisionState) string {
	if revision == nil {
		return ""
	}

	parts := make([]string, 0, 3)
	if revision.ID > 0 {
		parts = append(parts, fmt.Sprintf("%d", revision.ID))
	}
	if revision.SHA != "" {
		parts = append(parts, revision.SHA)
	}
	if revision.Status != "" {
		parts = append(parts, revision.Status)
	}
	return strings.Join(parts, " ")
}

func sanitizeCell(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}
