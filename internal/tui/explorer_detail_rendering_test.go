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
		"{} Body REQUIRED",
	}, "\n")

	styled := styleDetailSectionBadges(input)
	plain := normalizeViewportText(stripANSI(styled))
	if !strings.Contains(plain, "/: PATH") {
		t.Fatalf("expected styled output to keep path header text, got %q", plain)
	}
	if !strings.Contains(plain, "?& QUERY") {
		t.Fatalf("expected styled output to keep query header text, got %q", plain)
	}
	if !strings.Contains(plain, "{} BODY") {
		t.Fatalf("expected styled output to keep body header text, got %q", plain)
	}
	if !strings.Contains(plain, "{} BODY REQUIRED") {
		t.Fatalf("expected styled output to include required chip text, got %q", plain)
	}
	if styled == input {
		t.Fatalf("expected style function to add terminal styling escapes")
	}
}

func TestStyleDetailSectionBadgesNormalizesHeaderWhitespace(t *testing.T) {
	t.Parallel()

	input := strings.Join([]string{
		" /:  Path",
		" ?&   Query",
		" {}  Body",
		" {}  Body   REQUIRED",
	}, "\n")

	styled := styleDetailSectionBadges(input)
	plain := normalizeViewportText(stripANSI(styled))
	if !strings.Contains(plain, "/: PATH") {
		t.Fatalf("expected normalized styled path header, got %q", plain)
	}
	if !strings.Contains(plain, "?& QUERY") {
		t.Fatalf("expected normalized styled query header, got %q", plain)
	}
	if !strings.Contains(plain, "{} BODY") {
		t.Fatalf("expected normalized styled body header, got %q", plain)
	}
	if !strings.Contains(plain, "{} BODY REQUIRED") {
		t.Fatalf("expected normalized styled required body chip, got %q", plain)
	}
}

func TestStyleDetailSectionBadgesMatchesANSIWrappedHeaders(t *testing.T) {
	t.Parallel()

	input := strings.Join([]string{
		"\u001b[38;5;250m /:  Path\u001b[0m",
		"\u001b[38;5;250m ?&  Query\u001b[0m",
		"\u001b[38;5;250m {}   Body\u001b[0m",
		"\u001b[38;5;250m {}   Body   REQUIRED\u001b[0m",
	}, "\n")

	styled := styleDetailSectionBadges(input)
	plain := normalizeViewportText(stripANSI(styled))
	if !strings.Contains(plain, "/: PATH") || !strings.Contains(plain, "?& QUERY") || !strings.Contains(plain, "{} BODY") || !strings.Contains(plain, "{} BODY REQUIRED") {
		t.Fatalf("expected ansi-wrapped headers to be restyled, got %q", plain)
	}
}
