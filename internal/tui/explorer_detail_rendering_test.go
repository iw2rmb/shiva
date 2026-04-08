package tui

import (
	"strings"
	"testing"
)

func TestStyleDetailSectionBadgesStylesKnownHeaders(t *testing.T) {
	t.Parallel()

	input := strings.Join([]string{
		"/: Path",
		"?& Query",
		"{} Body",
	}, "\n")

	styled := styleDetailSectionBadges(input)
	plain := normalizeViewportText(stripANSI(styled))
	if !strings.Contains(plain, "/: Path") {
		t.Fatalf("expected styled output to keep path header text, got %q", plain)
	}
	if !strings.Contains(plain, "?& Query") {
		t.Fatalf("expected styled output to keep query header text, got %q", plain)
	}
	if !strings.Contains(plain, "{} Body") {
		t.Fatalf("expected styled output to keep body header text, got %q", plain)
	}
	if styled == input {
		t.Fatalf("expected style function to add terminal styling escapes")
	}
}
