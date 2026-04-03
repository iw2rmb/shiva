package tui

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/term"
)

func Run(
	ctx context.Context,
	input io.Reader,
	output io.Writer,
	service BrowserService,
	route InitialRoute,
	options RequestOptions,
) error {
	if service == nil {
		return fmt.Errorf("tui browser service is not configured")
	}
	if err := route.Validate(); err != nil {
		return err
	}
	if input == nil {
		return fmt.Errorf("tui input is not configured")
	}
	if output == nil {
		return fmt.Errorf("tui output is not configured")
	}

	model := newRootModel(service, route, options)
	programOptions := []tea.ProgramOption{
		tea.WithContext(ctx),
		tea.WithInput(input),
		tea.WithOutput(output),
	}
	if width, height, ok := detectInitialWindowSize(output, input); ok {
		model.width = width
		model.height = height
		model.resizeLists()
		model.refreshExplorerDetailViewport()
		programOptions = append(programOptions, tea.WithWindowSize(width, height))
	}

	program := tea.NewProgram(
		model,
		programOptions...,
	)

	_, err := program.Run()
	return err
}

func detectInitialWindowSize(output io.Writer, input io.Reader) (int, int, bool) {
	if width, height, ok := terminalSizeFromFD(output); ok {
		return width, height, true
	}
	if width, height, ok := terminalSizeFromFD(input); ok {
		return width, height, true
	}
	return terminalSizeFromEnv()
}

func terminalSizeFromFD(value any) (int, int, bool) {
	fdProvider, ok := value.(interface{ Fd() uintptr })
	if !ok {
		return 0, 0, false
	}
	fd := fdProvider.Fd()
	if fd == 0 || !term.IsTerminal(fd) {
		return 0, 0, false
	}
	width, height, err := term.GetSize(fd)
	if err != nil || width <= 0 || height <= 0 {
		return 0, 0, false
	}
	return width, height, true
}

func terminalSizeFromEnv() (int, int, bool) {
	width, widthErr := strconv.Atoi(strings.TrimSpace(os.Getenv("COLUMNS")))
	height, heightErr := strconv.Atoi(strings.TrimSpace(os.Getenv("LINES")))
	if widthErr != nil || heightErr != nil {
		return 0, 0, false
	}
	if width <= 0 || height <= 0 {
		return 0, 0, false
	}
	return width, height, true
}
