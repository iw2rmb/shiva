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
	want := "main (deadbeef), " + newListStyles(true).renderZeroOps("0 ops") + ", updated 12-03-2026 01:29:06"
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

func TestFormatOperationLinesSortsByPathAndFormatsLookupStyle(t *testing.T) {
	t.Parallel()

	rows := []clioutput.OperationRow{
		{Method: "post", Path: "/events/filter", OperationID: "searchEvents"},
		{Method: "get", Path: "/utilities/sendsay/requestId/{requestId}", OperationID: "getSendsayPreview"},
		{Method: "get", Path: "/event", OperationID: "getEvent"},
	}

	got := formatOperationLines(rows, false)
	want := []string{
		"GET /event                                   #getEvent",
		"POST /events/filter                          #searchEvents",
		"GET /utilities/sendsay/requestId/:requestId  #getSendsayPreview",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %#v, got %#v", want, got)
	}
}

func TestFormatOperationLinesStylesMethodsAndParamsForTTY(t *testing.T) {
	t.Parallel()

	rows := []clioutput.OperationRow{
		{Method: "get", Path: "/utilities/sendsay/requestId/{requestId}", OperationID: "getSendsayPreview"},
		{Method: "patch", Path: "/events/{eventId}", OperationID: "patchEvent"},
	}

	got := formatOperationLines(rows, true)
	styles := newListStyles(true)
	want := []string{
		styles.renderMethod("PATCH") + " /events/" + styles.renderPathParam(":eventId") + "                       #patchEvent",
		styles.renderMethod("GET") + " /utilities/sendsay/requestId/" + styles.renderPathParam(":requestId") + "  #getSendsayPreview",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %#v, got %#v", want, got)
	}
}
