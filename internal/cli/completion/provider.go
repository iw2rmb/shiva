package completion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/iw2rmb/shiva/internal/cli/catalog"
	"github.com/iw2rmb/shiva/internal/cli/config"
	"github.com/iw2rmb/shiva/internal/cli/httpclient"
	clioutput "github.com/iw2rmb/shiva/internal/cli/output"
	"github.com/iw2rmb/shiva/internal/cli/profile"
	"github.com/iw2rmb/shiva/internal/cli/request"
	"github.com/iw2rmb/shiva/internal/repoid"
	"github.com/spf13/cobra"
)

const defaultRefreshTimeout = 250 * time.Millisecond

var defaultHTTPMethods = []string{"delete", "get", "head", "options", "patch", "post", "put", "trace"}

type inventoryClient interface {
	ListRepos(ctx context.Context) ([]byte, error)
	GetCatalogStatus(ctx context.Context, repo string) ([]byte, error)
	ListAPIs(ctx context.Context, selector request.Envelope) ([]byte, error)
	ListOperations(ctx context.Context, selector request.Envelope) ([]byte, error)
}

type Provider struct {
	ResolvePaths   func() (config.Paths, error)
	LoadDocument   func(options config.LoadOptions) (config.Document, error)
	NewStore       func(cacheHome string) (*catalog.Store, error)
	NewClient      func(source profile.Source, timeout time.Duration) (inventoryClient, error)
	Now            func() time.Time
	RefreshTimeout time.Duration
}

type Selector struct {
	Namespace  string
	Repo       string
	API        string
	RevisionID int64
	SHA        string
}

func (s Selector) RepoPath() string {
	return repoid.Identity{Namespace: s.Namespace, Repo: s.Repo}.Path()
}

type PackedSelector struct {
	Namespace   string
	Repo        string
	Target      string
	OperationID string
}

func (s PackedSelector) RepoPath() string {
	return repoid.Identity{Namespace: s.Namespace, Repo: s.Repo}.Path()
}

type state struct {
	document config.Document
	store    *catalog.Store
}

func NewProvider() *Provider {
	return &Provider{
		ResolvePaths: config.ResolvePaths,
		LoadDocument: config.LoadDocument,
		NewStore:     catalog.NewStore,
		NewClient: func(source profile.Source, timeout time.Duration) (inventoryClient, error) {
			client, err := httpclient.New(httpclient.Config{
				BaseURL:        source.BaseURL,
				RequestTimeout: timeout,
				Token:          source.ResolvedToken(),
			})
			if err != nil {
				return nil, err
			}
			return &httpInventoryClient{client: client}, nil
		},
		Now:            time.Now,
		RefreshTimeout: defaultRefreshTimeout,
	}
}

func (p *Provider) CompleteRootArg(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	switch len(args) {
	case 0:
		switch {
		case strings.Contains(toComplete, "#"):
			return p.completePackedOperation(cmd.Context(), cmd, toComplete)
		case strings.Contains(toComplete, "@"):
			return p.completePackedTarget(cmd.Context(), cmd, toComplete)
		default:
			return p.completeRepoRefs(cmd.Context(), cmd, toComplete)
		}
	case 1:
		packed, err := parsePackedSelector(args[0])
		if err != nil || packed.OperationID != "" {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return completeHTTPMethods(toComplete), cobra.ShellCompDirectiveNoFileComp
	default:
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
}

func (p *Provider) CompleteRepoArg(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return p.completeRepoRefs(cmd.Context(), cmd, toComplete)
}

func (p *Provider) CompleteProfileFlag(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	st, err := p.loadState()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	values := make([]string, 0, len(st.document.Profiles))
	for name := range st.document.Profiles {
		values = append(values, name)
	}
	sort.Strings(values)
	return filterPrefix(values, toComplete), cobra.ShellCompDirectiveNoFileComp
}

func (p *Provider) CompleteTargetFlag(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	st, err := p.loadState()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	values := make([]string, 0, len(st.document.Targets))
	for name := range st.document.Targets {
		values = append(values, name)
	}
	sort.Strings(values)
	return filterPrefix(values, toComplete), cobra.ShellCompDirectiveNoFileComp
}

func (p *Provider) CompleteAPIFlag(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	selector, targetName, ok := p.selectorFromCommand(cmd, args)
	if !ok || strings.TrimSpace(selector.Repo) == "" {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	apis, _ := p.listAPIs(cmd.Context(), selector, p.profileName(cmd), targetName)
	return filterPrefix(apis, toComplete), cobra.ShellCompDirectiveNoFileComp
}

func (p *Provider) completeRepoRefs(ctx context.Context, cmd *cobra.Command, toComplete string) ([]string, cobra.ShellCompDirective) {
	repos, _ := p.listRepos(ctx, p.profileName(cmd), p.targetName(cmd))
	return filterPrefix(repos, toComplete), cobra.ShellCompDirectiveNoFileComp
}

func (p *Provider) completePackedTarget(ctx context.Context, cmd *cobra.Command, toComplete string) ([]string, cobra.ShellCompDirective) {
	repoPart, targetPart, found := strings.Cut(toComplete, "@")
	if !found {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	repoPart = strings.TrimSpace(repoPart)
	if repoPart == "" {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	targets, _ := p.CompleteTargetFlag(cmd, nil, targetPart)
	if len(targets) == 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	values := make([]string, 0, len(targets))
	for _, targetName := range targets {
		values = append(values, repoPart+"@"+targetName)
	}
	return values, cobra.ShellCompDirectiveNoFileComp
}

func (p *Provider) completePackedOperation(ctx context.Context, cmd *cobra.Command, toComplete string) ([]string, cobra.ShellCompDirective) {
	selectorPart, operationPart, found := strings.Cut(toComplete, "#")
	if !found {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	packed, err := parsePackedSelector(strings.TrimSuffix(toComplete, "#"+operationPart))
	if err != nil || strings.TrimSpace(packed.RepoPath()) == "" {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	selector, targetName, ok := p.selectorFromPacked(cmd, packed)
	if !ok {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	operationIDs, _ := p.listOperationIDs(ctx, selector, p.profileName(cmd), targetName)
	filtered := filterPrefix(operationIDs, operationPart)
	if len(filtered) == 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	values := make([]string, 0, len(filtered))
	for _, operationID := range filtered {
		values = append(values, selectorPart+"#"+operationID)
	}
	return values, cobra.ShellCompDirectiveNoFileComp
}

func (p *Provider) listRepos(ctx context.Context, profileName string, targetName string) ([]string, error) {
	st, source, err := p.loadSource(profileName, targetName)
	if err != nil {
		return nil, err
	}

	record, found, err := st.store.LoadRepos(source.Name)
	if err != nil {
		return nil, err
	}

	values := decodeRepoRefs(record.Payload)
	if !found || catalog.ReposRecordStale(record, p.now()) {
		refreshed, refreshErr := p.refreshRepos(ctx, st, source)
		if refreshErr == nil {
			values = refreshed
		}
	}

	return uniqueSorted(values), nil
}

func (p *Provider) listAPIs(ctx context.Context, selector Selector, profileName string, targetName string) ([]string, error) {
	st, source, err := p.loadSource(profileName, targetName)
	if err != nil {
		return nil, err
	}

	scope := catalog.ScopeFromSelector(selector.RevisionID, selector.SHA)
	record, found, err := st.store.LoadAPIs(source.Name, selector.RepoPath(), scope)
	if err != nil {
		return nil, err
	}

	values := decodeAPIs(record.Payload)
	if !found || p.snapshotSliceStale(st.store, source.Name, selector, record) {
		refreshed, refreshErr := p.refreshAPIs(ctx, st, source, selector)
		if refreshErr == nil {
			values = refreshed
		}
	}

	return uniqueSorted(values), nil
}

func (p *Provider) listOperationIDs(ctx context.Context, selector Selector, profileName string, targetName string) ([]string, error) {
	st, source, err := p.loadSource(profileName, targetName)
	if err != nil {
		return nil, err
	}

	scope := catalog.ScopeFromSelector(selector.RevisionID, selector.SHA)
	record, found, err := st.store.LoadOperations(source.Name, selector.RepoPath(), selector.API, scope)
	if err != nil {
		return nil, err
	}

	values := decodeOperationIDs(record.Payload)
	if !found || p.snapshotSliceStale(st.store, source.Name, selector, record) {
		refreshed, refreshErr := p.refreshOperations(ctx, st, source, selector)
		if refreshErr == nil {
			values = refreshed
		}
	}

	return uniqueSorted(values), nil
}

func (p *Provider) snapshotSliceStale(store *catalog.Store, profileName string, selector Selector, record catalog.Record) bool {
	scope := catalog.ScopeFromSelector(selector.RevisionID, selector.SHA)
	if !scope.Floating {
		return false
	}
	status, found, err := store.LoadStatus(profileName, selector.RepoPath())
	if err != nil || !found {
		return true
	}
	if catalog.StatusRecordStale(status, p.now()) {
		return true
	}
	return status.Fingerprint != record.Fingerprint
}

func (p *Provider) refreshRepos(ctx context.Context, st state, source profile.Source) ([]string, error) {
	client, err := p.newClient(source)
	if err != nil {
		return nil, err
	}

	service := catalog.NewService(st.store)
	refreshCtx, cancel := context.WithTimeout(ctx, p.refreshTimeout())
	defer cancel()

	if err := service.PrepareRepos(refreshCtx, client, source.Name, catalog.RefreshOptions{Refresh: true}); err != nil {
		return nil, err
	}

	record, found, err := st.store.LoadRepos(source.Name)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	return decodeRepoRefs(record.Payload), nil
}

func (p *Provider) refreshAPIs(ctx context.Context, st state, source profile.Source, selector Selector) ([]string, error) {
	client, err := p.newClient(source)
	if err != nil {
		return nil, err
	}

	service := catalog.NewService(st.store)
	refreshCtx, cancel := context.WithTimeout(ctx, p.refreshTimeout())
	defer cancel()

	if _, err := service.PrepareAPIs(refreshCtx, client, source.Name, selector.asEnvelope(), catalog.RefreshOptions{Refresh: true}); err != nil {
		return nil, err
	}

	record, found, err := st.store.LoadAPIs(source.Name, selector.RepoPath(), catalog.ScopeFromSelector(selector.RevisionID, selector.SHA))
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	return decodeAPIs(record.Payload), nil
}

func (p *Provider) refreshOperations(ctx context.Context, st state, source profile.Source, selector Selector) ([]string, error) {
	client, err := p.newClient(source)
	if err != nil {
		return nil, err
	}

	service := catalog.NewService(st.store)
	refreshCtx, cancel := context.WithTimeout(ctx, p.refreshTimeout())
	defer cancel()

	if _, err := service.PrepareOperations(refreshCtx, client, source.Name, selector.asEnvelope(), catalog.RefreshOptions{Refresh: true}); err != nil {
		return nil, err
	}

	record, found, err := st.store.LoadOperations(source.Name, selector.RepoPath(), selector.API, catalog.ScopeFromSelector(selector.RevisionID, selector.SHA))
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	return decodeOperationIDs(record.Payload), nil
}

func (p *Provider) loadState() (state, error) {
	if p == nil || p.ResolvePaths == nil || p.LoadDocument == nil || p.NewStore == nil {
		return state{}, errors.New("completion provider is not configured")
	}

	paths, err := p.ResolvePaths()
	if err != nil {
		return state{}, err
	}

	document, err := p.LoadDocument(config.LoadOptions{ConfigHome: paths.ConfigHome})
	if err != nil {
		return state{}, err
	}

	store, err := p.NewStore(paths.CacheHome)
	if err != nil {
		return state{}, err
	}

	return state{
		document: document,
		store:    store,
	}, nil
}

func (p *Provider) loadSource(profileName string, targetName string) (state, profile.Source, error) {
	st, err := p.loadState()
	if err != nil {
		return state{}, profile.Source{}, err
	}
	source, _, err := st.document.ResolveSource(profileName, targetName)
	if err != nil {
		return state{}, profile.Source{}, err
	}
	return st, source, nil
}

func (p *Provider) selectorFromCommand(cmd *cobra.Command, args []string) (Selector, string, bool) {
	if len(args) == 0 {
		return Selector{}, "", false
	}

	packed, err := parsePackedSelector(args[0])
	if err != nil {
		return Selector{}, "", false
	}
	return p.selectorFromPacked(cmd, packed)
}

func (p *Provider) selectorFromPacked(cmd *cobra.Command, packed PackedSelector) (Selector, string, bool) {
	if strings.TrimSpace(packed.RepoPath()) == "" {
		return Selector{}, "", false
	}

	revisionID, err := intFlag(cmd, "rev")
	if err != nil {
		return Selector{}, "", false
	}
	sha, err := stringFlag(cmd, "sha")
	if err != nil {
		return Selector{}, "", false
	}
	api, err := stringFlag(cmd, "api")
	if err != nil {
		return Selector{}, "", false
	}
	targetName, err := mergedTarget(cmd, packed.Target)
	if err != nil {
		return Selector{}, "", false
	}

	return Selector{
		Namespace:  packed.Namespace,
		Repo:       packed.Repo,
		API:        api,
		RevisionID: revisionID,
		SHA:        sha,
	}, targetName, true
}

func (p *Provider) profileName(cmd *cobra.Command) string {
	value, err := stringFlag(cmd, "profile")
	if err != nil {
		return ""
	}
	return value
}

func (p *Provider) targetName(cmd *cobra.Command) string {
	value, err := stringFlag(cmd, "via")
	if err != nil {
		return ""
	}
	return value
}

func (p *Provider) newClient(source profile.Source) (inventoryClient, error) {
	if p == nil || p.NewClient == nil {
		return nil, errors.New("completion provider is not configured")
	}
	return p.NewClient(source, p.refreshTimeout())
}

func (p *Provider) now() time.Time {
	if p == nil || p.Now == nil {
		return time.Now().UTC()
	}
	return p.Now().UTC()
}

func (p *Provider) refreshTimeout() time.Duration {
	if p == nil || p.RefreshTimeout <= 0 {
		return defaultRefreshTimeout
	}
	return p.RefreshTimeout
}

func parsePackedSelector(raw string) (PackedSelector, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return PackedSelector{}, errors.New("selector must not be empty")
	}

	repoAndTarget, operationID, _ := strings.Cut(value, "#")
	repoPath, target, _ := strings.Cut(strings.TrimSpace(repoAndTarget), "@")
	repoPath = strings.TrimSpace(repoPath)
	target = strings.TrimSpace(target)
	operationID = strings.TrimSpace(operationID)
	if repoPath == "" {
		return PackedSelector{}, errors.New("repo path must not be empty")
	}
	identity, err := repoid.ParsePath(repoPath)
	if err != nil {
		return PackedSelector{}, err
	}
	return PackedSelector{
		Namespace:   identity.Namespace,
		Repo:        identity.Repo,
		Target:      target,
		OperationID: operationID,
	}, nil
}

func mergedTarget(cmd *cobra.Command, packedTarget string) (string, error) {
	flagTarget, err := stringFlag(cmd, "via")
	if err != nil {
		return "", err
	}

	packedTarget = strings.TrimSpace(packedTarget)
	flagTarget = strings.TrimSpace(flagTarget)
	switch {
	case packedTarget == "":
		return flagTarget, nil
	case flagTarget == "":
		return packedTarget, nil
	case packedTarget == flagTarget:
		return packedTarget, nil
	default:
		return "", errors.New("packed target and --via do not match")
	}
}

func stringFlag(cmd *cobra.Command, name string) (string, error) {
	if cmd == nil {
		return "", errors.New("command is not configured")
	}
	flag := cmd.Flag(name)
	if flag == nil {
		return "", fmt.Errorf("flag %q is not configured", name)
	}
	return strings.TrimSpace(flag.Value.String()), nil
}

func intFlag(cmd *cobra.Command, name string) (int64, error) {
	if cmd == nil {
		return 0, errors.New("command is not configured")
	}
	flag := cmd.Flag(name)
	if flag == nil {
		return 0, fmt.Errorf("flag %q is not configured", name)
	}
	value := strings.TrimSpace(flag.Value.String())
	if value == "" {
		return 0, nil
	}
	return strconv.ParseInt(value, 10, 64)
}

func completeHTTPMethods(toComplete string) []string {
	values := make([]string, 0, len(defaultHTTPMethods))
	for _, method := range defaultHTTPMethods {
		if strings.HasPrefix(strings.ToLower(method), strings.ToLower(strings.TrimSpace(toComplete))) {
			values = append(values, method)
		}
	}
	return values
}

func filterPrefix(values []string, prefix string) []string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return uniqueSorted(values)
	}

	filtered := make([]string, 0, len(values))
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			filtered = append(filtered, value)
		}
	}
	return uniqueSorted(filtered)
}

func uniqueSorted(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	sort.Strings(unique)
	return unique
}

func decodeRepoRefs(payload []byte) []string {
	var rows []clioutput.RepoRow
	if err := json.Unmarshal(payload, &rows); err != nil {
		return nil
	}
	values := make([]string, 0, len(rows))
	for _, row := range rows {
		values = append(values, repoid.Identity{Namespace: row.Namespace, Repo: row.Repo}.Path())
	}
	return values
}

func decodeAPIs(payload []byte) []string {
	var rows []clioutput.APIRow
	if err := json.Unmarshal(payload, &rows); err != nil {
		return nil
	}
	values := make([]string, 0, len(rows))
	for _, row := range rows {
		values = append(values, row.API)
	}
	return values
}

func decodeOperationIDs(payload []byte) []string {
	var rows []clioutput.OperationRow
	if err := json.Unmarshal(payload, &rows); err != nil {
		return nil
	}
	values := make([]string, 0, len(rows))
	for _, row := range rows {
		values = append(values, row.OperationID)
	}
	return values
}

func (s Selector) asEnvelope() request.Envelope {
	return request.Envelope{
		Namespace:  s.Namespace,
		Repo:       s.Repo,
		API:        s.API,
		RevisionID: s.RevisionID,
		SHA:        s.SHA,
	}
}

type httpInventoryClient struct {
	client *httpclient.Client
}

func (c *httpInventoryClient) ListRepos(ctx context.Context) ([]byte, error) {
	return c.client.ListRepos(ctx)
}

func (c *httpInventoryClient) GetCatalogStatus(ctx context.Context, repo string) ([]byte, error) {
	return c.client.GetCatalogStatus(ctx, repo)
}

func (c *httpInventoryClient) ListAPIs(ctx context.Context, selector request.Envelope) ([]byte, error) {
	return c.client.ListAPIs(ctx, selector)
}

func (c *httpInventoryClient) ListOperations(ctx context.Context, selector request.Envelope) ([]byte, error) {
	return c.client.ListOperations(ctx, selector)
}
