package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	clioutput "github.com/iw2rmb/shiva/internal/cli/output"
)

func TestMethodChipWithAlignedGapUsesFixedNineCellMethodColumn(t *testing.T) {
	t.Parallel()

	getPrefix := methodChipWithAlignedGap("get")
	optionsPrefix := methodChipWithAlignedGap("options")

	if got := lipgloss.Width(getPrefix); got != 9 {
		t.Fatalf("expected GET prefix width 9, got %d", got)
	}
	if got := lipgloss.Width(optionsPrefix); got != 9 {
		t.Fatalf("expected OPTIONS prefix width 9, got %d", got)
	}
	if !strings.HasPrefix(getPrefix, strings.Repeat(" ", 4)) {
		t.Fatalf("expected GET prefix to have 4-space left margin, got %q", getPrefix)
	}
}

func TestEndpointItemsDescriptionUsesOperationIDAndSummary(t *testing.T) {
	t.Parallel()

	items := endpointItems([]EndpointEntry{
		{
			Identity: EndpointIdentity{
				Method:      "options",
				Path:        "/pets",
				OperationID: "endpoint",
			},
			Row: clioutput.OperationRow{Summary: "Describe endpoint"},
		},
		{
			Identity: EndpointIdentity{
				Method: "get",
				Path:   "/pets",
			},
			Row: clioutput.OperationRow{Summary: "List pets"},
		},
	})

	if len(items) != 2 {
		t.Fatalf("expected 2 endpoint items, got %d", len(items))
	}

	first, ok := items[0].(endpointListItem)
	if !ok {
		t.Fatalf("expected first item to be endpointListItem, got %T", items[0])
	}
	if first.Description() != "          #endpoint Describe endpoint" {
		t.Fatalf("expected operation id and summary subtitle, got %q", first.Description())
	}

	second, ok := items[1].(endpointListItem)
	if !ok {
		t.Fatalf("expected second item to be endpointListItem, got %T", items[1])
	}
	if second.Description() != "          List pets" {
		t.Fatalf("expected summary-only subtitle, got %q", second.Description())
	}
	if strings.Contains(second.Description(), "#") {
		t.Fatalf("expected no operation id prefix in summary-only subtitle, got %q", second.Description())
	}
}

func TestEndpointItemsDescriptionDoesNotUseEndpointFallback(t *testing.T) {
	t.Parallel()

	items := endpointItems([]EndpointEntry{
		{
			Identity: EndpointIdentity{
				Method: "get",
				Path:   "/pets",
			},
		},
	})
	if len(items) != 1 {
		t.Fatalf("expected 1 endpoint item, got %d", len(items))
	}
	item, ok := items[0].(endpointListItem)
	if !ok {
		t.Fatalf("expected endpointListItem, got %T", items[0])
	}
	if item.Description() != "" {
		t.Fatalf("expected empty subtitle when operation id and summary are absent, got %q", item.Description())
	}
}
