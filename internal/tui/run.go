package tui

import (
	"context"
	"fmt"
	"io"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	clioutput "github.com/iw2rmb/shiva/internal/cli/output"
	"github.com/iw2rmb/shiva/internal/cli/request"
)

type RequestOptions struct {
	Profile string
	Offline bool
}

type SpecFormat string

const (
	SpecFormatJSON SpecFormat = "json"
	SpecFormatYAML SpecFormat = "yaml"
)

type BrowserService interface {
	ListRepos(ctx context.Context, options RequestOptions, format clioutput.ListFormat) ([]byte, error)
	ListOperations(ctx context.Context, selector request.Envelope, options RequestOptions, format clioutput.ListFormat) ([]byte, error)
	GetOperation(ctx context.Context, selector request.Envelope, options RequestOptions) ([]byte, error)
	GetSpec(ctx context.Context, selector request.Envelope, options RequestOptions, format SpecFormat) ([]byte, error)
}

type rootModel struct {
	service BrowserService
	route   InitialRoute
	options RequestOptions
	width   int
	height  int
}

func newRootModel(service BrowserService, route InitialRoute, options RequestOptions) rootModel {
	return rootModel{
		service: service,
		route:   route,
		options: options,
	}
}

func (model rootModel) Init() tea.Cmd {
	return nil
}

func (model rootModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch typed := msg.(type) {
	case tea.KeyMsg:
		switch typed.String() {
		case "ctrl+c", "q":
			return model, tea.Quit
		}
	case tea.WindowSizeMsg:
		model.width = typed.Width
		model.height = typed.Height
	}

	return model, nil
}

func (model rootModel) View() string {
	lines := []string{
		"Shiva TUI",
		"",
		routeLabel(model.route),
	}
	if model.options.Profile != "" {
		lines = append(lines, "profile: "+model.options.Profile)
	}
	if model.options.Offline {
		lines = append(lines, "offline: true")
	}
	if model.width > 0 || model.height > 0 {
		lines = append(lines, fmt.Sprintf("size: %dx%d", model.width, model.height))
	}
	lines = append(lines, "", "Press q to quit.")
	return strings.Join(lines, "\n")
}

func routeLabel(route InitialRoute) string {
	switch route.Kind {
	case RouteNamespaces:
		return "start: namespaces"
	case RouteRepos:
		return "start: namespace " + route.Namespace
	case RouteRepoExplorer:
		return "start: repo " + route.Namespace + "/" + route.Repo
	default:
		return "start: unknown"
	}
}

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
