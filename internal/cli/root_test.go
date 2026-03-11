package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/iw2rmb/shiva/internal/cli/catalog"
	"github.com/iw2rmb/shiva/internal/cli/output"
	"github.com/iw2rmb/shiva/internal/cli/request"
	"github.com/spf13/cobra"
)

func TestRootCommandDispatchesShorthandForms(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name            string
		args            []string
		expectedStdout  string
		expectedSpec    int
		expectedOp      int
		expectedCall    int
		expectedFormat  SpecFormat
		expectedCallFmt CallFormat
		expectedRequest request.Envelope
	}{
		{
			name:           "repo selector",
			args:           []string{"allure/allure-deployment"},
			expectedStdout: "openapi: 3.1.0\n",
			expectedSpec:   1,
			expectedFormat: SpecFormatYAML,
			expectedRequest: request.Envelope{
				Kind: request.KindSpec,
				Repo: "allure/allure-deployment",
			},
		},
		{
			name:           "operation selector by id",
			args:           []string{"allure/allure-deployment#findAll_42"},
			expectedStdout: "{\"operationId\":\"findAll_42\"}\n",
			expectedOp:     1,
			expectedRequest: request.Envelope{
				Kind:        request.KindOperation,
				Repo:        "allure/allure-deployment",
				OperationID: "findAll_42",
			},
		},
		{
			name:           "operation selector by method path with yaml output",
			args:           []string{"-o", "yaml", "allure/allure-deployment", "PATCH", "/pets/:id"},
			expectedStdout: "operationId: patchPet\n",
			expectedOp:     1,
			expectedRequest: request.Envelope{
				Kind:   request.KindOperation,
				Repo:   "allure/allure-deployment",
				Method: "patch",
				Path:   "/pets/{id}",
			},
		},
		{
			name:            "call selector by target",
			args:            []string{"--dry-run", "allure/allure-deployment@shiva#getUsers"},
			expectedStdout:  "{\"kind\":\"call\"}\n",
			expectedCall:    1,
			expectedCallFmt: CallFormatJSON,
			expectedRequest: request.Envelope{
				Kind:        request.KindCall,
				Repo:        "allure/allure-deployment",
				Target:      "shiva",
				OperationID: "getUsers",
				DryRun:      true,
			},
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			service := &fakeService{
				specBody:      []byte("openapi: 3.1.0\n"),
				operationBody: []byte(`{"operationId":"findAll_42"}`),
				callBody:      []byte(`{"kind":"call"}`),
			}
			if testCase.expectedRequest.Method == "patch" {
				service.operationBody = []byte(`{"operationId":"patchPet"}`)
			}

			stdout := &bytes.Buffer{}
			stderr := &bytes.Buffer{}
			command := NewRootCommand(func() (Service, error) {
				return service, nil
			})
			command.SetOut(stdout)
			command.SetErr(stderr)
			command.SetArgs(testCase.args)

			if err := command.ExecuteContext(context.Background()); err != nil {
				t.Fatalf("execute command failed: %v", err)
			}
			if stdout.String() != testCase.expectedStdout {
				t.Fatalf("expected stdout %q, got %q", testCase.expectedStdout, stdout.String())
			}
			if service.specCalls != testCase.expectedSpec {
				t.Fatalf("expected spec calls %d, got %d", testCase.expectedSpec, service.specCalls)
			}
			if service.operationCalls != testCase.expectedOp {
				t.Fatalf("expected operation calls %d, got %d", testCase.expectedOp, service.operationCalls)
			}
			if service.callCalls != testCase.expectedCall {
				t.Fatalf("expected call execution calls %d, got %d", testCase.expectedCall, service.callCalls)
			}
			if testCase.expectedFormat != "" && service.lastFormat != testCase.expectedFormat {
				t.Fatalf("expected format %q, got %q", testCase.expectedFormat, service.lastFormat)
			}
			if testCase.expectedCallFmt != "" && service.lastCallFormat != testCase.expectedCallFmt {
				t.Fatalf("expected call format %q, got %q", testCase.expectedCallFmt, service.lastCallFormat)
			}
			if !reflect.DeepEqual(service.lastRequest, testCase.expectedRequest) {
				t.Fatalf("expected request %+v, got %+v", testCase.expectedRequest, service.lastRequest)
			}
			if stderr.Len() != 0 {
				t.Fatalf("expected empty stderr, got %q", stderr.String())
			}
		})
	}
}

func TestRootCommandCompletionDoesNotLoadService(t *testing.T) {
	t.Parallel()

	loadCalls := 0
	stdout := &bytes.Buffer{}
	command := NewRootCommand(func() (Service, error) {
		loadCalls++
		return &fakeService{}, nil
	})
	command.SetOut(stdout)
	command.SetErr(&bytes.Buffer{})
	command.SetArgs([]string{"completion", "bash"})

	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute completion command failed: %v", err)
	}
	if loadCalls != 0 {
		t.Fatalf("expected completion command to avoid loading service, got %d load calls", loadCalls)
	}
	if stdout.Len() == 0 {
		t.Fatalf("expected completion command to emit a script")
	}
}

func TestRootCommandHealthUsesService(t *testing.T) {
	t.Parallel()

	service := &fakeService{
		healthBody: []byte(`{"status":"ok"}`),
	}

	stdout := &bytes.Buffer{}
	command := NewRootCommand(func() (Service, error) {
		return service, nil
	})
	command.SetOut(stdout)
	command.SetErr(&bytes.Buffer{})
	command.SetArgs([]string{"health"})

	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute health command failed: %v", err)
	}
	if service.healthCalls != 1 {
		t.Fatalf("expected one health call, got %d", service.healthCalls)
	}
	if stdout.String() != "{\"status\":\"ok\"}\n" {
		t.Fatalf("unexpected health stdout %q", stdout.String())
	}
}

func TestRootCommandListAndSyncSubcommands(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name               string
		args               []string
		expectedStdout     string
		expectedListRepos  int
		expectedListAPIs   int
		expectedListOps    int
		expectedSync       int
		expectedFormat     string
		expectListTarget   bool
		expectedListTarget request.Envelope
	}{
		{
			name:              "ls repos defaults to ndjson on non tty",
			args:              []string{"ls", "repos"},
			expectedStdout:    "{\"repo\":\"acme/platform\"}\n",
			expectedListRepos: 1,
			expectedFormat:    "ndjson",
		},
		{
			name:             "ls apis accepts snapshot selector",
			args:             []string{"ls", "apis", "--rev", "42", "-o", "json", "acme/platform"},
			expectedStdout:   "[{\"repo\":\"acme/platform\",\"api\":\"apis/pets/openapi.yaml\"}]\n",
			expectedListAPIs: 1,
			expectedFormat:   "json",
			expectListTarget: true,
			expectedListTarget: request.Envelope{
				Repo:       "acme/platform",
				RevisionID: 42,
			},
		},
		{
			name:             "ls ops forwards api selector",
			args:             []string{"ls", "ops", "--api", "apis/pets/openapi.yaml", "-o", "tsv", "acme/platform"},
			expectedStdout:   "repo\tapi\tmethod\tpath\toperation_id\tdeprecated\tsummary\n",
			expectedListOps:  1,
			expectedFormat:   "tsv",
			expectListTarget: true,
			expectedListTarget: request.Envelope{
				Repo: "acme/platform",
				API:  "apis/pets/openapi.yaml",
			},
		},
		{
			name:             "sync refreshes repo snapshot",
			args:             []string{"sync", "--rev", "42", "acme/platform"},
			expectedStdout:   "{\"repo\":\"acme/platform\",\"scope\":\"rev:42\"}\n",
			expectedSync:     1,
			expectListTarget: true,
			expectedListTarget: request.Envelope{
				Repo:       "acme/platform",
				RevisionID: 42,
			},
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			service := &fakeService{
				listReposBody: []byte("{\"repo\":\"acme/platform\"}\n"),
				listAPIsBody:  []byte("[{\"repo\":\"acme/platform\",\"api\":\"apis/pets/openapi.yaml\"}]"),
				listOpsBody:   []byte("repo\tapi\tmethod\tpath\toperation_id\tdeprecated\tsummary\n"),
				syncBody:      []byte("{\"repo\":\"acme/platform\",\"scope\":\"rev:42\"}"),
			}

			stdout := &bytes.Buffer{}
			command := NewRootCommand(func() (Service, error) {
				return service, nil
			})
			command.SetOut(stdout)
			command.SetErr(&bytes.Buffer{})
			command.SetArgs(testCase.args)

			if err := command.ExecuteContext(context.Background()); err != nil {
				t.Fatalf("execute command failed: %v", err)
			}
			if stdout.String() != testCase.expectedStdout {
				t.Fatalf("expected stdout %q, got %q", testCase.expectedStdout, stdout.String())
			}
			if service.listReposCalls != testCase.expectedListRepos {
				t.Fatalf("expected list repos calls %d, got %d", testCase.expectedListRepos, service.listReposCalls)
			}
			if service.listAPIsCalls != testCase.expectedListAPIs {
				t.Fatalf("expected list apis calls %d, got %d", testCase.expectedListAPIs, service.listAPIsCalls)
			}
			if service.listOpsCalls != testCase.expectedListOps {
				t.Fatalf("expected list ops calls %d, got %d", testCase.expectedListOps, service.listOpsCalls)
			}
			if service.syncCalls != testCase.expectedSync {
				t.Fatalf("expected sync calls %d, got %d", testCase.expectedSync, service.syncCalls)
			}
			if testCase.expectedFormat != "" && service.lastListFormat != testCase.expectedFormat {
				t.Fatalf("expected list format %q, got %q", testCase.expectedFormat, service.lastListFormat)
			}
			if testCase.expectListTarget && !reflect.DeepEqual(service.lastListRequest, testCase.expectedListTarget) {
				t.Fatalf("expected list request %+v, got %+v", testCase.expectedListTarget, service.lastListRequest)
			}
		})
	}
}

func TestRootCommandListAndSyncRejectRefreshOfflineCombination(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		args []string
	}{
		{
			name: "ls repos",
			args: []string{"ls", "repos", "--refresh", "--offline"},
		},
		{
			name: "sync",
			args: []string{"sync", "--refresh", "--offline", "acme/platform"},
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			command := NewRootCommand(func() (Service, error) {
				return &fakeService{}, nil
			})
			command.SetOut(&bytes.Buffer{})
			command.SetErr(&bytes.Buffer{})
			command.SetArgs(testCase.args)

			err := command.ExecuteContext(context.Background())
			if err == nil {
				t.Fatalf("expected invalid input error")
			}
			if err.Error() != "--refresh and --offline are mutually exclusive" {
				t.Fatalf("unexpected error %q", err.Error())
			}
		})
	}
}

func TestRootCommandListNDJSONLeavesEmptyResultsEmpty(t *testing.T) {
	t.Parallel()

	command := NewRootCommand(func() (Service, error) {
		return &fakeService{}, nil
	})
	stdout := &bytes.Buffer{}
	command.SetOut(stdout)
	command.SetErr(&bytes.Buffer{})
	command.SetArgs([]string{"ls", "repos"})

	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute command failed: %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected empty ndjson output, got %q", stdout.String())
	}
}

func TestRootCommandDynamicCompletionsUseCatalogAndConfig(t *testing.T) {
	configHome := t.TempDir()
	cacheHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("XDG_CACHE_HOME", cacheHome)

	configPath := configHome + "/shiva/profiles.yaml"
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("create config dir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte(`
active_profile: default
profiles:
  default:
    base_url: http://127.0.0.1:8080
    timeout: 10s
  local:
    base_url: http://127.0.0.1:9090
    timeout: 10s
targets:
  prod:
    mode: direct
    base_url: https://api.example
    timeout: 10s
`), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	store, err := catalog.NewStore(cacheHome)
	if err != nil {
		t.Fatalf("create catalog store: %v", err)
	}
	scope := catalog.ScopeFromSelector(0, "")
	fingerprint := catalog.SnapshotFingerprint{RevisionID: 42, SHA: "deadbeef"}
	if err := store.SaveRepos("default", []byte(`[{"repo":"acme/platform"}]`)); err != nil {
		t.Fatalf("save repos cache: %v", err)
	}
	if err := store.SaveStatus("default", "acme/platform", []byte(`{"repo":"acme/platform","snapshot_revision":{"id":42,"sha":"deadbeef"}}`), fingerprint); err != nil {
		t.Fatalf("save status cache: %v", err)
	}
	if err := store.SaveAPIs("default", "acme/platform", scope, []byte(`[{"api":"apis/pets/openapi.yaml","has_snapshot":true}]`), fingerprint); err != nil {
		t.Fatalf("save apis cache: %v", err)
	}
	if err := store.SaveOperations("default", "acme/platform", "", scope, []byte(`[{"api":"apis/pets/openapi.yaml","method":"get","path":"/pets","operation_id":"getPets"}]`), fingerprint); err != nil {
		t.Fatalf("save operation cache: %v", err)
	}

	root := NewRootCommand(func() (Service, error) {
		return &fakeService{}, nil
	})

	repos, directive := root.ValidArgsFunction(root, nil, "ac")
	if directive != cobra.ShellCompDirectiveNoFileComp || !reflect.DeepEqual(repos, []string{"acme/platform"}) {
		t.Fatalf("unexpected repo completion %#v directive %v", repos, directive)
	}

	ops, directive := root.ValidArgsFunction(root, nil, "acme/platform#get")
	if directive != cobra.ShellCompDirectiveNoFileComp || !reflect.DeepEqual(ops, []string{"acme/platform#getPets"}) {
		t.Fatalf("unexpected operation completion %#v directive %v", ops, directive)
	}

	methods, directive := root.ValidArgsFunction(root, []string{"acme/platform"}, "po")
	if directive != cobra.ShellCompDirectiveNoFileComp || !reflect.DeepEqual(methods, []string{"post"}) {
		t.Fatalf("unexpected method completion %#v directive %v", methods, directive)
	}

	profileCompletion, ok := root.GetFlagCompletionFunc("profile")
	if !ok {
		t.Fatalf("expected profile completion function")
	}
	profiles, directive := profileCompletion(root, nil, "l")
	if directive != cobra.ShellCompDirectiveNoFileComp || !reflect.DeepEqual(profiles, []string{"local"}) {
		t.Fatalf("unexpected profile completion %#v directive %v", profiles, directive)
	}

	targetCompletion, ok := root.GetFlagCompletionFunc("via")
	if !ok {
		t.Fatalf("expected target completion function")
	}
	targets, directive := targetCompletion(root, nil, "p")
	if directive != cobra.ShellCompDirectiveNoFileComp || !reflect.DeepEqual(targets, []string{"prod"}) {
		t.Fatalf("unexpected target completion %#v directive %v", targets, directive)
	}

	apiCompletion, ok := root.GetFlagCompletionFunc("api")
	if !ok {
		t.Fatalf("expected api completion function")
	}
	apis, directive := apiCompletion(root, []string{"acme/platform"}, "apis/")
	if directive != cobra.ShellCompDirectiveNoFileComp || !reflect.DeepEqual(apis, []string{"apis/pets/openapi.yaml"}) {
		t.Fatalf("unexpected api completion %#v directive %v", apis, directive)
	}

	syncCmd, _, err := root.Find([]string{"sync"})
	if err != nil {
		t.Fatalf("find sync command: %v", err)
	}
	syncRepos, directive := syncCmd.ValidArgsFunction(syncCmd, nil, "ac")
	if directive != cobra.ShellCompDirectiveNoFileComp || !reflect.DeepEqual(syncRepos, []string{"acme/platform"}) {
		t.Fatalf("unexpected sync repo completion %#v directive %v", syncRepos, directive)
	}
}

type fakeService struct {
	specBody        []byte
	operationBody   []byte
	callBody        []byte
	listReposBody   []byte
	listAPIsBody    []byte
	listOpsBody     []byte
	emitReposBody   []byte
	emitAPIsBody    []byte
	emitOpsBody     []byte
	syncBody        []byte
	healthBody      []byte
	specCalls       int
	operationCalls  int
	callCalls       int
	listReposCalls  int
	listAPIsCalls   int
	listOpsCalls    int
	emitReposCalls  int
	emitAPIsCalls   int
	emitOpsCalls    int
	syncCalls       int
	healthCalls     int
	lastRequest     request.Envelope
	lastListRequest request.Envelope
	lastFormat      SpecFormat
	lastCallFormat  CallFormat
	lastListFormat  string
	lastOptions     RequestOptions
}

func (s *fakeService) GetSpec(ctx context.Context, selector request.Envelope, options RequestOptions, format SpecFormat) ([]byte, error) {
	s.specCalls++
	s.lastRequest = selector
	s.lastFormat = format
	s.lastOptions = options
	return s.specBody, nil
}

func (s *fakeService) GetOperation(ctx context.Context, selector request.Envelope, options RequestOptions) ([]byte, error) {
	s.operationCalls++
	s.lastRequest = selector
	s.lastOptions = options
	return s.operationBody, nil
}

func (s *fakeService) ExecuteCall(ctx context.Context, selector request.Envelope, options RequestOptions, format CallFormat) ([]byte, error) {
	s.callCalls++
	s.lastRequest = selector
	s.lastCallFormat = format
	s.lastOptions = options
	return s.callBody, nil
}

func (s *fakeService) ListRepos(ctx context.Context, options RequestOptions, format output.ListFormat) ([]byte, error) {
	s.listReposCalls++
	s.lastListFormat = string(format)
	s.lastOptions = options
	return s.listReposBody, nil
}

func (s *fakeService) ListAPIs(ctx context.Context, selector request.Envelope, options RequestOptions, format output.ListFormat) ([]byte, error) {
	s.listAPIsCalls++
	s.lastListRequest = selector
	s.lastListFormat = string(format)
	s.lastOptions = options
	return s.listAPIsBody, nil
}

func (s *fakeService) ListOperations(ctx context.Context, selector request.Envelope, options RequestOptions, format output.ListFormat) ([]byte, error) {
	s.listOpsCalls++
	s.lastListRequest = selector
	s.lastListFormat = string(format)
	s.lastOptions = options
	return s.listOpsBody, nil
}

func (s *fakeService) EmitRepoRequests(ctx context.Context, options RequestOptions) ([]byte, error) {
	s.emitReposCalls++
	s.lastOptions = options
	return s.emitReposBody, nil
}

func (s *fakeService) EmitAPIRequests(ctx context.Context, selector request.Envelope, options RequestOptions) ([]byte, error) {
	s.emitAPIsCalls++
	s.lastListRequest = selector
	s.lastOptions = options
	return s.emitAPIsBody, nil
}

func (s *fakeService) EmitOperationRequests(ctx context.Context, selector request.Envelope, options RequestOptions, targetName string) ([]byte, error) {
	s.emitOpsCalls++
	s.lastListRequest = selector
	s.lastOptions = options
	_ = targetName
	return s.emitOpsBody, nil
}

func (s *fakeService) Sync(ctx context.Context, selector request.Envelope, options RequestOptions) ([]byte, error) {
	s.syncCalls++
	s.lastListRequest = selector
	s.lastOptions = options
	return s.syncBody, nil
}

func (s *fakeService) Health(ctx context.Context, options RequestOptions) ([]byte, error) {
	s.healthCalls++
	s.lastOptions = options
	return s.healthBody, nil
}
