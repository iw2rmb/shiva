package cli

import (
	"reflect"
	"testing"

	clioutput "github.com/iw2rmb/shiva/internal/cli/output"
)

func TestFormatRepoSummaryLeavesNonTTYOutputUnchanged(t *testing.T) {
	t.Parallel()

	got := formatRepoSummary("main", "deadbeef", 0, "updated 12-03-2026 01:29:06", false, false)
	want := "main (deadbeef), 0 ops, updated 12-03-2026 01:29:06"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestFormatRepoSummaryDimsZeroOpsForTTY(t *testing.T) {
	t.Parallel()

	got := formatRepoSummary("main", "deadbeef", 0, "updated 12-03-2026 01:29:06", false, true)
	want := newListStyles(true).renderDimmed("main (deadbeef), 0 ops, updated 12-03-2026 01:29:06")
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestFormatRepoSummaryLeavesNonZeroOpsUndimmedForTTY(t *testing.T) {
	t.Parallel()

	got := formatRepoSummary("main", "deadbeef", 3, "updated 12-03-2026 01:29:06", true, true)
	want := "main (deadbeef), total 3 ops, updated 12-03-2026 01:29:06"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestRepoPendingLabelDimsTTYOutput(t *testing.T) {
	t.Parallel()

	got := repoPendingLabel(clioutput.RepoRow{}, true)
	want := newListStyles(true).renderDimmed("pending")
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestRepoPendingLabelLeavesNonTTYOutputPlain(t *testing.T) {
	t.Parallel()

	got := repoPendingLabel(clioutput.RepoRow{}, false)
	if got != "pending" {
		t.Fatalf("expected %q, got %q", "pending", got)
	}
}

func TestRepoListSummaryDimsRepoNameForZeroOps(t *testing.T) {
	t.Parallel()

	summary, dimmed, err := repoListSummary(
		t.Context(),
		nil,
		RequestOptions{},
		clioutput.RepoRow{
			Repo: "infosys-marketplace-linguistic",
			HeadRevision: &clioutput.RevisionState{
				SHA:    "4209a977",
				Status: "processed",
			},
		},
		false,
		true,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !dimmed {
		t.Fatalf("expected repo name to be dimmed for zero-op summary")
	}
	wantSummary := newListStyles(true).renderDimmed("4209a977, 0 ops")
	if summary != wantSummary {
		t.Fatalf("expected %q, got %q", wantSummary, summary)
	}
}

func TestFormatOperationLinesSortsByPathAndFormatsLookupStyle(t *testing.T) {
	t.Parallel()

	rows := []clioutput.OperationRow{
		{Method: "post", Path: "/events/filter", OperationID: "searchEvents", Summary: "Desc2"},
		{Method: "get", Path: "/utilities/sendsay/requestId/{requestId}", OperationID: "getSendsayPreview", Summary: "Desc3"},
		{Method: "get", Path: "/event", OperationID: "getEvent", Summary: "Desc1"},
	}

	got := formatOperationLines(rows, false)
	want := []string{
		" GET /event                                   #getEvent           Desc1",
		"",
		"POST /events/filter                           #searchEvents       Desc2",
		"",
		" GET /utilities/sendsay/requestId/:requestId  #getSendsayPreview  Desc3",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %#v, got %#v", want, got)
	}
}

func TestFormatOperationLinesStylesMethodsAndParamsForTTY(t *testing.T) {
	t.Parallel()

	rows := []clioutput.OperationRow{
		{Method: "get", Path: "/utilities/sendsay/requestId/{requestId}", OperationID: "getSendsayPreview", Summary: "Show preview"},
		{Method: "patch", Path: "/events/{eventId}", OperationID: "patchEvent", Summary: "Update event"},
	}

	got := formatOperationLines(rows, true)
	styles := newListStyles(true)
	want := []string{
		styles.renderMethod("PATCH") + " /events/" + styles.renderPathParam(":eventId") + "                         " + styles.renderOperationID("#patchEvent") + "         " + styles.renderSummary("Update event"),
		"",
		"  " + styles.renderMethod("GET") + " /utilities/sendsay/requestId/" + styles.renderPathParam(":requestId") + "  " + styles.renderOperationID("#getSendsayPreview") + "  " + styles.renderSummary("Show preview"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %#v, got %#v", want, got)
	}
}
