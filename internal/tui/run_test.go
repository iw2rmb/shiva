package tui

import "testing"

func TestTerminalSizeFromEnv(t *testing.T) {
	t.Setenv("COLUMNS", "120")
	t.Setenv("LINES", "40")

	width, height, ok := terminalSizeFromEnv()
	if !ok {
		t.Fatalf("expected environment size to parse")
	}
	if width != 120 || height != 40 {
		t.Fatalf("expected 120x40, got %dx%d", width, height)
	}
}

func TestTerminalSizeFromEnvRejectsInvalidValues(t *testing.T) {
	t.Setenv("COLUMNS", "abc")
	t.Setenv("LINES", "0")

	if _, _, ok := terminalSizeFromEnv(); ok {
		t.Fatalf("expected invalid environment size to be rejected")
	}
}
