package tui

import (
	"strings"
	"testing"

	"charm.land/glamour/v2"
)

func TestResolveGlamourStyleUsesEnvOverride(t *testing.T) {
	t.Setenv("GLAMOUR_STYLE", "light")

	if got := resolveGlamourStyle(); got != "light" {
		t.Fatalf("expected style %q, got %q", "light", got)
	}
}

func TestResolveGlamourStyleUsesDefaultWhenUnset(t *testing.T) {
	t.Setenv("GLAMOUR_STYLE", "")

	if got := resolveGlamourStyle(); got != defaultGlamourStyle {
		t.Fatalf("expected default style %q, got %q", defaultGlamourStyle, got)
	}
}

func TestGlamourRendererCachesRendererByWrapWidth(t *testing.T) {
	renderer := &glamourMarkdownRenderer{
		style:     defaultGlamourStyle,
		renderers: make(map[int]*glamour.TermRenderer),
	}

	first := renderer.Render("## hello", 80)
	if first == "" {
		t.Fatalf("expected rendered markdown to be non-empty")
	}
	if got := len(renderer.renderers); got != 1 {
		t.Fatalf("expected one cached renderer, got %d", got)
	}

	second := renderer.Render("## world", 80)
	if second == "" {
		t.Fatalf("expected rendered markdown to be non-empty")
	}
	if got := len(renderer.renderers); got != 1 {
		t.Fatalf("expected cached renderer count to remain 1, got %d", got)
	}

	_ = renderer.Render("## narrow", 10)
	if got := len(renderer.renderers); got != 2 {
		t.Fatalf("expected second cached renderer for clamped width, got %d", got)
	}
}

func TestTrimSingleLeadingIndentRemovesAtMostOneLeadingSpacePerLine(t *testing.T) {
	input := strings.Join([]string{
		"  first",
		" second",
		"third",
	}, "\n")

	got := trimSingleLeadingIndent(input)
	want := strings.Join([]string{
		" first",
		"second",
		"third",
	}, "\n")
	if got != want {
		t.Fatalf("expected trimmed value %q, got %q", want, got)
	}
}

func TestGlamourRendererPreservesSingleNewlines(t *testing.T) {
	renderer := &glamourMarkdownRenderer{
		style:     defaultGlamourStyle,
		renderers: make(map[int]*glamour.TermRenderer),
	}

	rendered := renderer.Render(strings.Join([]string{
		"Status: active",
		"Ingest: master (97da8339) @ -",
		"Revision: 3480",
	}, "\n"), 120)
	plain := stripANSI(rendered)

	statusIdx := strings.Index(plain, "Status: active")
	ingestIdx := strings.Index(plain, "Ingest: master (97da8339) @ -")
	revisionIdx := strings.Index(plain, "Revision: 3480")
	if statusIdx < 0 || ingestIdx < 0 || revisionIdx < 0 {
		t.Fatalf("expected all rows present, got %q", plain)
	}

	if strings.Count(plain[statusIdx:ingestIdx], "\n") < 1 {
		t.Fatalf("expected newline between status and ingest rows, got %q", plain)
	}
	if strings.Count(plain[ingestIdx:revisionIdx], "\n") < 1 {
		t.Fatalf("expected newline between ingest and revision rows, got %q", plain)
	}
}
