package tui

import (
	"context"
	"fmt"
	"io"

	tea "github.com/charmbracelet/bubbletea"
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

	program := tea.NewProgram(
		newRootModel(service, route, options),
		tea.WithContext(ctx),
		tea.WithInput(input),
		tea.WithOutput(output),
		tea.WithAltScreen(),
	)

	_, err := program.Run()
	return err
}
