package cli

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/iw2rmb/shiva/internal/tui"
)

func TestTUICommandParsesValidEntryRoutes(t *testing.T) {
	testCases := []struct {
		name           string
		args           []string
		expectedRoute  tui.InitialRoute
		expectedOption tui.RequestOptions
	}{
		{
			name:          "root tui starts in namespaces",
			args:          []string{"tui"},
			expectedRoute: tui.InitialRoute{Kind: tui.RouteNamespaces},
		},
		{
			name:          "namespace selector starts in repo list",
			args:          []string{"tui", "acme/"},
			expectedRoute: tui.InitialRoute{Kind: tui.RouteRepos, Namespace: "acme"},
		},
		{
			name:          "repo selector starts in repo explorer",
			args:          []string{"tui", "--profile", "work", "--offline", "acme/platform"},
			expectedRoute: tui.InitialRoute{Kind: tui.RouteRepoExplorer, Namespace: "acme", Repo: "platform"},
			expectedOption: tui.RequestOptions{
				Profile: "work",
				Offline: true,
			},
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			service := &fakeService{}
			var gotRoute tui.InitialRoute
			var gotOptions tui.RequestOptions
			var loadCalls int

			previousRun := runTUI
			t.Cleanup(func() {
				runTUI = previousRun
			})
			runTUI = func(
				ctx context.Context,
				input io.Reader,
				output io.Writer,
				browserService tui.BrowserService,
				route tui.InitialRoute,
				options tui.RequestOptions,
			) error {
				_ = ctx
				_ = input
				_ = output
				if browserService == nil {
					t.Fatalf("expected browser service")
				}
				gotRoute = route
				gotOptions = options
				return nil
			}

			command := NewRootCommand(func() (Service, error) {
				loadCalls++
				return service, nil
			})
			command.SetIn(bytes.NewBuffer(nil))
			command.SetOut(&bytes.Buffer{})
			command.SetErr(&bytes.Buffer{})
			command.SetArgs(testCase.args)

			if err := command.ExecuteContext(context.Background()); err != nil {
				t.Fatalf("execute tui command failed: %v", err)
			}
			if loadCalls != 1 {
				t.Fatalf("expected one service load, got %d", loadCalls)
			}
			if !reflect.DeepEqual(gotRoute, testCase.expectedRoute) {
				t.Fatalf("expected route %+v, got %+v", testCase.expectedRoute, gotRoute)
			}
			if !reflect.DeepEqual(gotOptions, testCase.expectedOption) {
				t.Fatalf("expected options %+v, got %+v", testCase.expectedOption, gotOptions)
			}
		})
	}
}

func TestTUICommandRejectsUnsupportedFlags(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "api", args: []string{"tui", "--api", "pets"}, wantErr: "tui does not accept --api"},
		{name: "rev", args: []string{"tui", "--rev", "42"}, wantErr: "tui does not accept --sha or --rev"},
		{name: "rev negative", args: []string{"tui", "--rev", "-1"}, wantErr: "tui does not accept --sha or --rev"},
		{name: "sha", args: []string{"tui", "--sha", "deadbeef"}, wantErr: "tui does not accept --sha or --rev"},
		{name: "target", args: []string{"tui", "--via", "prod"}, wantErr: "tui does not accept --via"},
		{name: "dry-run", args: []string{"tui", "--dry-run"}, wantErr: "tui does not accept --dry-run"},
		{name: "output", args: []string{"tui", "--output", "json"}, wantErr: "tui does not accept --output"},
		{name: "path", args: []string{"tui", "--path", "id=1"}, wantErr: "tui does not accept --path, --query, --header, --json, or --body"},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			command := NewRootCommand(func() (Service, error) {
				t.Fatalf("service should not be loaded on validation failure")
				return nil, nil
			})
			command.SetIn(bytes.NewBuffer(nil))
			command.SetOut(&bytes.Buffer{})
			command.SetErr(&bytes.Buffer{})
			command.SetArgs(testCase.args)

			err := command.ExecuteContext(context.Background())
			if err == nil {
				t.Fatalf("expected validation error")
			}
			if err.Error() != testCase.wantErr {
				t.Fatalf("expected error %q, got %q", testCase.wantErr, err.Error())
			}
		})
	}
}

func TestTUICommandRejectsUnsupportedSelectors(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "empty", args: []string{"tui", " "}, wantErr: "tui selector must not be empty"},
		{name: "namespace without slash", args: []string{"tui", "acme"}, wantErr: "tui selector must be <namespace>/ or <namespace>/<repo>"},
		{name: "namespace with repeated slash", args: []string{"tui", "acme//"}, wantErr: "tui namespace must not be empty"},
		{name: "operation selector", args: []string{"tui", "acme/platform#getPet"}, wantErr: "tui selector must be <namespace>/ or <namespace>/<repo>"},
		{name: "target selector", args: []string{"tui", "acme/platform@prod"}, wantErr: "tui selector must be <namespace>/ or <namespace>/<repo>"},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			command := NewRootCommand(func() (Service, error) {
				t.Fatalf("service should not be loaded on selector validation failure")
				return nil, nil
			})
			command.SetIn(bytes.NewBuffer(nil))
			command.SetOut(&bytes.Buffer{})
			command.SetErr(&bytes.Buffer{})
			command.SetArgs(testCase.args)

			err := command.ExecuteContext(context.Background())
			if err == nil {
				t.Fatalf("expected selector validation error")
			}
			if err.Error() != testCase.wantErr {
				t.Fatalf("expected error %q, got %q", testCase.wantErr, err.Error())
			}
		})
	}
}

func TestTUICommandIsRegisteredOnRoot(t *testing.T) {
	t.Parallel()

	command := NewRootCommand(func() (Service, error) {
		return &fakeService{}, nil
	})

	subcommands := command.Commands()
	names := make([]string, 0, len(subcommands))
	for _, subcommand := range subcommands {
		names = append(names, subcommand.Name())
	}

	if !strings.Contains(strings.Join(names, ","), "tui") {
		t.Fatalf("expected root command to register tui subcommand, got %v", names)
	}
}
