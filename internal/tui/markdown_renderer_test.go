package tui

import (
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
