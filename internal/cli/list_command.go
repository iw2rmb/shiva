package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/iw2rmb/shiva/internal/cli/completion"
	clioutput "github.com/iw2rmb/shiva/internal/cli/output"
	"github.com/iw2rmb/shiva/internal/cli/request"
	"github.com/iw2rmb/shiva/internal/repoid"
	"github.com/spf13/cobra"
)

type listSelectionKind string

const (
	listSelectionNamespacesAll   listSelectionKind = "namespaces_all"
	listSelectionNamespacesMatch listSelectionKind = "namespaces_match"
	listSelectionNamespaceRepos  listSelectionKind = "namespace_repos"
	listSelectionRepoMatch       listSelectionKind = "repo_match"
	listSelectionRepoOperations  listSelectionKind = "repo_operations"
)

type listSelection struct {
	Kind       listSelectionKind
	Prefix     string
	Namespace  string
	RepoPrefix string
	Repo       string
}

type namespaceEntry struct {
	Namespace  string
	RepoCount  int
	AllPending bool
}

func newListCommand(serviceFactory func() (Service, error), flags *RootFlags, completionProvider *completion.Provider) *cobra.Command {
	command := &cobra.Command{
		Use:               "ls [selector]",
		Short:             "Browse Shiva catalog",
		SilenceUsage:      true,
		SilenceErrors:     true,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completionProvider.CompleteRepoArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateListFlags(*flags); err != nil {
				return err
			}

			service, err := loadService(serviceFactory)
			if err != nil {
				return err
			}

			selector := ""
			if len(args) == 1 {
				selector = strings.TrimSpace(args[0])
			}

			body, err := executeListCommand(
				cmd.Context(),
				service,
				selector,
				requestOptionsFromFlags(*flags),
				writerSupportsANSI(cmd.OutOrStdout()),
			)
			if err != nil {
				return err
			}
			return writeOutput(cmd.OutOrStdout(), body)
		},
	}
	return command
}

func requestOptionsFromFlags(flags RootFlags) RequestOptions {
	return RequestOptions{
		Profile: flags.Profile,
		Offline: flags.Offline,
	}
}

func validateListFlags(flags RootFlags) error {
	switch {
	case flags.API != "":
		return &InvalidInputError{Message: "ls does not accept --api"}
	case flags.SHA != "" || flags.RevisionID > 0:
		return &InvalidInputError{Message: "ls does not accept --sha or --rev"}
	case flags.Target != "":
		return &InvalidInputError{Message: "ls does not accept --via"}
	case flags.DryRun:
		return &InvalidInputError{Message: "ls does not accept --dry-run"}
	case flags.Output != "":
		return &InvalidInputError{Message: "ls does not accept --output"}
	default:
		return validateNoCallInputFlags(flags, "ls")
	}
}

func executeListCommand(
	ctx context.Context,
	service Service,
	rawSelector string,
	options RequestOptions,
	colorize bool,
) ([]byte, error) {
	repoRows, err := loadListRepoRows(ctx, service, options)
	if err != nil {
		return nil, err
	}

	selection, err := resolveListSelection(rawSelector, repoRows)
	if err != nil {
		return nil, err
	}

	switch selection.Kind {
	case listSelectionNamespacesAll:
		return renderNamespaceEntries(namespaceEntriesFromRows(repoRows), false), nil
	case listSelectionNamespacesMatch:
		return renderNamespaceEntries(filterNamespaceEntries(namespaceEntriesFromRows(repoRows), selection.Prefix), true), nil
	case listSelectionNamespaceRepos:
		return renderNamespaceRepos(ctx, service, options, selection.Namespace, "", repoRows, colorize), nil
	case listSelectionRepoMatch:
		return renderNamespaceRepos(ctx, service, options, selection.Namespace, selection.RepoPrefix, repoRows, colorize), nil
	case listSelectionRepoOperations:
		return renderRepoOperations(ctx, service, options, selection.Namespace, selection.Repo, repoRows, colorize)
	default:
		return nil, fmt.Errorf("unsupported list selection %q", selection.Kind)
	}
}

func loadListRepoRows(ctx context.Context, service Service, options RequestOptions) ([]clioutput.RepoRow, error) {
	body, err := service.ListRepos(ctx, options, clioutput.ListFormatJSON)
	if err != nil {
		return nil, err
	}

	var rows []clioutput.RepoRow
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("decode repo list: %w", err)
	}
	sort.Slice(rows, func(i, j int) bool {
		left := repoid.Identity{Namespace: rows[i].Namespace, Repo: rows[i].Repo}.Path()
		right := repoid.Identity{Namespace: rows[j].Namespace, Repo: rows[j].Repo}.Path()
		return left < right
	})
	return rows, nil
}

func resolveListSelection(raw string, repoRows []clioutput.RepoRow) (listSelection, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return listSelection{Kind: listSelectionNamespacesAll}, nil
	}
	if strings.HasSuffix(raw, "/") {
		return listSelection{
			Kind:      listSelectionNamespaceRepos,
			Namespace: strings.TrimSuffix(raw, "/"),
		}, nil
	}
	if repoRowByPath(repoRows, raw) != nil {
		identity, err := repoid.ParsePath(raw)
		if err != nil {
			return listSelection{}, &InvalidInputError{Message: err.Error()}
		}
		return listSelection{
			Kind:      listSelectionRepoOperations,
			Namespace: identity.Namespace,
			Repo:      identity.Repo,
		}, nil
	}
	if hasNamespacePrefix(repoRows, raw) {
		return listSelection{
			Kind:   listSelectionNamespacesMatch,
			Prefix: raw,
		}, nil
	}
	identity, err := repoid.ParsePath(raw)
	if err != nil {
		return listSelection{}, &InvalidInputError{Message: err.Error()}
	}
	return listSelection{
		Kind:       listSelectionRepoMatch,
		Namespace:  identity.Namespace,
		RepoPrefix: identity.Repo,
	}, nil
}

func namespaceEntriesFromRows(rows []clioutput.RepoRow) []namespaceEntry {
	if len(rows) == 0 {
		return nil
	}

	summaries := make(map[string]namespaceEntry)
	for _, row := range rows {
		entry := summaries[row.Namespace]
		entry.Namespace = row.Namespace
		entry.RepoCount++
		if entry.RepoCount == 1 {
			entry.AllPending = repoRowIsPending(row)
		} else {
			entry.AllPending = entry.AllPending && repoRowIsPending(row)
		}
		summaries[row.Namespace] = entry
	}

	out := make([]namespaceEntry, 0, len(summaries))
	for _, entry := range summaries {
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Namespace < out[j].Namespace
	})
	return out
}

func filterNamespaceEntries(entries []namespaceEntry, prefix string) []namespaceEntry {
	filtered := make([]namespaceEntry, 0, len(entries))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Namespace, prefix) {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func hasNamespacePrefix(rows []clioutput.RepoRow, prefix string) bool {
	for _, row := range rows {
		if strings.HasPrefix(row.Namespace, prefix) {
			return true
		}
	}
	return false
}

func repoRowByPath(rows []clioutput.RepoRow, path string) *clioutput.RepoRow {
	for index := range rows {
		identity := repoid.Identity{Namespace: rows[index].Namespace, Repo: rows[index].Repo}
		if identity.Path() == path {
			return &rows[index]
		}
	}
	return nil
}

func renderNamespaceEntries(entries []namespaceEntry, matched bool) []byte {
	buffer := &bytes.Buffer{}
	if matched {
		fmt.Fprintf(buffer, "match: %d\n", len(entries))
	} else {
		fmt.Fprintf(buffer, "total: %d\n", len(entries))
	}

	writer := tabwriter.NewWriter(buffer, 0, 0, 2, ' ', 0)
	for _, entry := range entries {
		description := fmt.Sprintf("%d %s", entry.RepoCount, pluralize(entry.RepoCount, "repo", "repos"))
		if entry.AllPending {
			description += ", all pending"
		}
		fmt.Fprintf(writer, "%s\t%s\n", entry.Namespace, description)
	}
	_ = writer.Flush()
	return buffer.Bytes()
}

func renderNamespaceRepos(
	ctx context.Context,
	service Service,
	options RequestOptions,
	namespace string,
	repoPrefix string,
	repoRows []clioutput.RepoRow,
	colorize bool,
) []byte {
	namespaceRows := rowsByNamespace(repoRows, namespace)
	matchedRows := namespaceRows
	headerLabel := fmt.Sprintf("total %d repos", len(namespaceRows))
	if repoPrefix != "" {
		matchedRows = filterRowsByRepoPrefix(namespaceRows, repoPrefix)
		headerLabel = fmt.Sprintf("match %d repos", len(matchedRows))
	}

	buffer := &bytes.Buffer{}
	fmt.Fprintf(buffer, "namespace %s, %s\n", namespace, headerLabel)

	writer := tabwriter.NewWriter(buffer, 0, 0, 2, ' ', 0)
	for _, row := range matchedRows {
		description, err := repoListSummary(ctx, service, options, row, false, colorize)
		if err != nil {
			description = repoFallbackSummary(row, false, colorize)
		}
		fmt.Fprintf(writer, "%s\t%s\n", row.Repo, description)
	}
	_ = writer.Flush()
	return buffer.Bytes()
}

func renderRepoOperations(
	ctx context.Context,
	service Service,
	options RequestOptions,
	namespace string,
	repo string,
	repoRows []clioutput.RepoRow,
	colorize bool,
) ([]byte, error) {
	namespaceRows := rowsByNamespace(repoRows, namespace)
	identity := repoid.Identity{Namespace: namespace, Repo: repo}
	row := repoRowByPath(namespaceRows, identity.Path())
	if row == nil {
		return nil, &NotFoundError{Message: fmt.Sprintf("repo %q was not found", identity.Path())}
	}

	description, operationRows, err := repoOperationSummary(ctx, service, options, *row, colorize)
	if err != nil {
		return nil, err
	}

	buffer := &bytes.Buffer{}
	fmt.Fprintf(buffer, "namespace %s, total %d repos\n", namespace, len(namespaceRows))
	writer := tabwriter.NewWriter(buffer, 0, 0, 2, ' ', 0)
	fmt.Fprintf(writer, "%s\t%s\n", row.Repo, description)
	_ = writer.Flush()

	for _, line := range formatOperationLines(operationRows, colorize) {
		fmt.Fprintln(buffer, line)
	}

	return buffer.Bytes(), nil
}

func rowsByNamespace(rows []clioutput.RepoRow, namespace string) []clioutput.RepoRow {
	filtered := make([]clioutput.RepoRow, 0)
	for _, row := range rows {
		if row.Namespace == namespace {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

func filterRowsByRepoPrefix(rows []clioutput.RepoRow, prefix string) []clioutput.RepoRow {
	filtered := make([]clioutput.RepoRow, 0)
	for _, row := range rows {
		if strings.HasPrefix(row.Repo, prefix) {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

func repoListSummary(
	ctx context.Context,
	service Service,
	options RequestOptions,
	row clioutput.RepoRow,
	exact bool,
	colorize bool,
) (string, error) {
	if repoRowIsPending(row) {
		return repoPendingLabel(row, colorize), nil
	}
	if row.ActiveAPICount < 1 {
		return formatRepoSummary("", shortSHA(repoHeadSHA(row)), 0, repoUpdatedLabel(row), exact, colorize), nil
	}

	apiRows, err := loadRepoAPIRows(ctx, service, options, row)
	if err != nil {
		return "", err
	}

	branch, sha, totalOps := repoAPISummary(apiRows)
	return formatRepoSummary(branch, shortSHA(sha), totalOps, repoUpdatedLabel(row), exact, colorize), nil
}

func repoOperationSummary(
	ctx context.Context,
	service Service,
	options RequestOptions,
	row clioutput.RepoRow,
	colorize bool,
) (string, []clioutput.OperationRow, error) {
	if repoRowIsPending(row) {
		return repoPendingLabel(row, colorize), nil, nil
	}

	operationRows, err := loadRepoOperationRows(ctx, service, options, row)
	if err != nil {
		return "", nil, err
	}

	branch, sha := repoOperationMetadata(operationRows)
	if branch == "" && sha == "" && row.ActiveAPICount > 0 {
		apiRows, apiErr := loadRepoAPIRows(ctx, service, options, row)
		if apiErr == nil {
			branch, sha, _ = repoAPISummary(apiRows)
		}
	}

	return formatRepoSummary(branch, shortSHA(sha), len(operationRows), repoUpdatedLabel(row), true, colorize), operationRows, nil
}

func loadRepoAPIRows(
	ctx context.Context,
	service Service,
	options RequestOptions,
	row clioutput.RepoRow,
) ([]clioutput.APIRow, error) {
	body, err := service.ListAPIs(ctx, request.Envelope{
		Namespace: row.Namespace,
		Repo:      row.Repo,
	}, options, clioutput.ListFormatJSON)
	if err != nil {
		return nil, err
	}

	var apiRows []clioutput.APIRow
	if err := json.Unmarshal(body, &apiRows); err != nil {
		return nil, fmt.Errorf("decode api list: %w", err)
	}
	return apiRows, nil
}

func loadRepoOperationRows(
	ctx context.Context,
	service Service,
	options RequestOptions,
	row clioutput.RepoRow,
) ([]clioutput.OperationRow, error) {
	body, err := service.ListOperations(ctx, request.Envelope{
		Namespace: row.Namespace,
		Repo:      row.Repo,
	}, options, clioutput.ListFormatJSON)
	if err != nil {
		return nil, err
	}

	var operationRows []clioutput.OperationRow
	if err := json.Unmarshal(body, &operationRows); err != nil {
		return nil, fmt.Errorf("decode operation list: %w", err)
	}
	return operationRows, nil
}

func repoAPISummary(rows []clioutput.APIRow) (string, string, int) {
	branch := ""
	sha := ""
	totalOps := 0
	for _, row := range rows {
		totalOps += int(row.OperationCount)
		if branch == "" && strings.TrimSpace(row.IngestEventBranch) != "" {
			branch = strings.TrimSpace(row.IngestEventBranch)
		}
		if sha == "" && strings.TrimSpace(row.IngestEventSHA) != "" {
			sha = strings.TrimSpace(row.IngestEventSHA)
		}
	}
	return branch, sha, totalOps
}

func repoOperationMetadata(rows []clioutput.OperationRow) (string, string) {
	for _, row := range rows {
		branch := strings.TrimSpace(row.IngestEventBranch)
		sha := strings.TrimSpace(row.IngestEventSHA)
		if branch != "" || sha != "" {
			return branch, sha
		}
	}
	return "", ""
}

func formatOperationLines(rows []clioutput.OperationRow, colorize bool) []string {
	if len(rows) == 0 {
		return nil
	}
	styles := newListStyles(colorize)

	sortedRows := append([]clioutput.OperationRow(nil), rows...)
	sort.SliceStable(sortedRows, func(i, j int) bool {
		leftPath := operationDisplayPathPlain(sortedRows[i].Path)
		rightPath := operationDisplayPathPlain(sortedRows[j].Path)
		switch {
		case leftPath != rightPath:
			return leftPath < rightPath
		case strings.TrimSpace(strings.ToLower(sortedRows[i].Method)) != strings.TrimSpace(strings.ToLower(sortedRows[j].Method)):
			return strings.TrimSpace(strings.ToLower(sortedRows[i].Method)) < strings.TrimSpace(strings.ToLower(sortedRows[j].Method))
		default:
			return strings.TrimSpace(sortedRows[i].OperationID) < strings.TrimSpace(sortedRows[j].OperationID)
		}
	})

	maxMethodWidth := 0
	maxPathWidth := 0
	maxOperationIDWidth := 0
	for _, row := range sortedRows {
		method := operationMethodLabel(row, newListStyles(false), false)
		if width := renderedWidth(method); width > maxMethodWidth {
			maxMethodWidth = width
		}
		path := operationDisplayPath(row.Path, newListStyles(false))
		if width := renderedWidth(path); width > maxPathWidth {
			maxPathWidth = width
		}
		operationID := operationIDLabel(row, newListStyles(false))
		if width := renderedWidth(operationID); width > maxOperationIDWidth {
			maxOperationIDWidth = width
		}
	}

	lines := make([]string, 0, len(sortedRows))
	previousGroup := ""
	for index, row := range sortedRows {
		group := operationPrimaryPathSegment(row.Path)
		if index > 0 && group != previousGroup {
			lines = append(lines, "")
		}
		lines = append(lines, formatOperationLine(row, styles, maxMethodWidth, maxPathWidth, maxOperationIDWidth))
		previousGroup = group
	}
	return lines
}

func formatOperationLine(row clioutput.OperationRow, styles listStyles, methodWidth int, pathWidth int, operationIDWidth int) string {
	methodPlain := operationMethodLabel(row, newListStyles(false), false)
	methodStyled := operationMethodLabel(row, styles, true)
	pathPlain := operationDisplayPath(row.Path, newListStyles(false))
	pathStyled := operationDisplayPath(row.Path, styles)
	operationIDPlain := operationIDLabel(row, newListStyles(false))
	operationIDStyled := operationIDLabel(row, styles)
	summary := strings.TrimSpace(row.Summary)

	var buffer strings.Builder
	buffer.Grow(methodWidth + pathWidth + operationIDWidth + len(summary) + 8)
	buffer.WriteString(strings.Repeat(" ", max(0, methodWidth-renderedWidth(methodPlain))))
	buffer.WriteString(methodStyled)
	buffer.WriteString(" ")
	buffer.WriteString(pathStyled)
	buffer.WriteString(strings.Repeat(" ", max(0, pathWidth-renderedWidth(pathPlain))+2))
	if operationIDPlain != "" {
		buffer.WriteString(operationIDStyled)
		if summary != "" {
			buffer.WriteString(strings.Repeat(" ", max(0, operationIDWidth-renderedWidth(operationIDPlain))+2))
		}
	} else if summary != "" {
		buffer.WriteString(strings.Repeat(" ", operationIDWidth+2))
	}
	if summary != "" {
		buffer.WriteString(styles.renderSummary(summary))
	}
	return buffer.String()
}

func operationMethodLabel(row clioutput.OperationRow, styles listStyles, colorize bool) string {
	method := strings.ToUpper(strings.TrimSpace(row.Method))
	if colorize {
		return styles.renderMethod(method)
	}
	return method
}

func operationIDLabel(row clioutput.OperationRow, styles listStyles) string {
	operationID := strings.TrimSpace(row.OperationID)
	if operationID == "" {
		return ""
	}
	return styles.renderOperationID("#" + operationID)
}

func operationDisplayPath(path string, styles listStyles) string {
	segments := strings.Split(strings.TrimSpace(path), "/")
	if len(segments) == 0 {
		return "/"
	}

	display := make([]string, 0, len(segments))
	for index, segment := range segments {
		if index == 0 && segment == "" {
			display = append(display, "")
			continue
		}
		if len(segment) > 2 && strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}") {
			segment = ":" + strings.TrimSpace(segment[1:len(segment)-1])
			segment = styles.renderPathParam(segment)
		}
		display = append(display, segment)
	}

	joined := strings.Join(display, "/")
	if joined == "" {
		return "/"
	}
	if strings.HasPrefix(joined, "/") {
		return joined
	}
	return "/" + joined
}

func operationDisplayPathPlain(path string) string {
	return operationDisplayPath(path, newListStyles(false))
}

func operationPrimaryPathSegment(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" || trimmed == "/" {
		return ""
	}
	trimmed = strings.TrimPrefix(trimmed, "/")
	if trimmed == "" {
		return ""
	}
	segments := strings.Split(trimmed, "/")
	return segments[0]
}

func formatRepoSummary(branch string, sha string, ops int, updated string, exact bool, colorize bool) string {
	styles := newListStyles(colorize)
	parts := make([]string, 0, 3)
	ref := formatBranchSHA(branch, sha)
	if ref != "" {
		parts = append(parts, ref)
	}
	parts = append(parts, formatRepoOpsLabel(ops, exact))
	if updated != "" {
		parts = append(parts, updated)
	}
	summary := strings.Join(parts, ", ")
	if ops == 0 && colorize {
		return styles.renderDimmed(summary)
	}
	return summary
}

func formatRepoOpsLabel(ops int, exact bool) string {
	label := fmt.Sprintf("%d ops", ops)
	if exact {
		label = "total " + label
	}
	return label
}

func formatBranchSHA(branch string, sha string) string {
	branch = strings.TrimSpace(branch)
	sha = strings.TrimSpace(sha)
	switch {
	case branch != "" && sha != "":
		return fmt.Sprintf("%s (%s)", branch, sha)
	case branch != "":
		return branch
	case sha != "":
		return sha
	default:
		return ""
	}
}

func repoFallbackSummary(row clioutput.RepoRow, exact bool, colorize bool) string {
	if repoRowIsPending(row) {
		return repoPendingLabel(row, colorize)
	}
	return formatRepoSummary("", shortSHA(repoHeadSHA(row)), 0, repoUpdatedLabel(row), exact, colorize)
}

func repoPendingLabel(row clioutput.RepoRow, colorize bool) string {
	styles := newListStyles(colorize)
	if row.HeadRevision == nil {
		return styles.renderDimmed("pending")
	}
	status := strings.TrimSpace(strings.ToLower(row.HeadRevision.Status))
	switch status {
	case "processing":
		return styles.renderDimmed("processing")
	default:
		return styles.renderDimmed("pending")
	}
}

func repoRowIsPending(row clioutput.RepoRow) bool {
	if row.HeadRevision == nil {
		return false
	}
	switch strings.TrimSpace(strings.ToLower(row.HeadRevision.Status)) {
	case "pending", "processing":
		return true
	default:
		return false
	}
}

func repoUpdatedLabel(row clioutput.RepoRow) string {
	timestamp, ok := repoUpdatedAt(row)
	if !ok {
		return ""
	}
	return "updated " + timestamp.Format("02-01-2006 15:04:05")
}

func repoUpdatedAt(row clioutput.RepoRow) (time.Time, bool) {
	candidates := []*time.Time{}
	if row.HeadRevision != nil {
		candidates = append(candidates, row.HeadRevision.ProcessedAt, row.HeadRevision.ReceivedAt)
	}
	if row.SnapshotRevision != nil {
		candidates = append(candidates, row.SnapshotRevision.ProcessedAt, row.SnapshotRevision.ReceivedAt)
	}

	var latest time.Time
	for _, candidate := range candidates {
		if candidate == nil || candidate.IsZero() {
			continue
		}
		if latest.IsZero() || candidate.After(latest) {
			latest = *candidate
		}
	}
	if latest.IsZero() {
		return time.Time{}, false
	}
	return latest.UTC(), true
}

func repoHeadSHA(row clioutput.RepoRow) string {
	if row.HeadRevision == nil {
		return ""
	}
	return strings.TrimSpace(row.HeadRevision.SHA)
}

func shortSHA(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 8 {
		return value
	}
	return value[:8]
}

func pluralize(count int, singular string, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
}

func max(left int, right int) int {
	if left > right {
		return left
	}
	return right
}

func writerSupportsANSI(writer io.Writer) bool {
	file, ok := writer.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func executeSyncCommand(ctx context.Context, service Service, selector request.Envelope, flags RootFlags) ([]byte, error) {
	if flags.Output != "" && flags.Output != "json" {
		return nil, &InvalidInputError{Message: "sync output must be json"}
	}
	return service.Sync(ctx, selector, requestOptionsFromFlags(flags))
}

func parseRepoSnapshotSelector(raw string, flags RootFlags, allowAPI bool, allowTarget bool) (request.Envelope, error) {
	packed, err := ParsePackedSelector(raw)
	if err != nil {
		return request.Envelope{}, err
	}
	if packed.HasTarget() {
		return request.Envelope{}, &InvalidInputError{Message: "this command does not accept @target selectors"}
	}
	if packed.HasOperation() {
		return request.Envelope{}, &InvalidInputError{Message: "this command does not accept #<operation-id> selectors"}
	}
	if flags.Target != "" && !allowTarget {
		return request.Envelope{}, &InvalidInputError{Message: "this command does not accept --via"}
	}
	if flags.DryRun {
		return request.Envelope{}, &InvalidInputError{Message: "this command does not accept --dry-run"}
	}
	if err := validateNoCallInputFlags(flags, "this command"); err != nil {
		return request.Envelope{}, err
	}
	if !allowAPI && flags.API != "" {
		return request.Envelope{}, &InvalidInputError{Message: "this command does not accept --api"}
	}

	return request.Envelope{
		Namespace:  packed.Namespace,
		Repo:       packed.Repo,
		API:        flags.API,
		RevisionID: flags.RevisionID,
		SHA:        flags.SHA,
	}, nil
}

func newSyncCommand(serviceFactory func() (Service, error), flags *RootFlags) *cobra.Command {
	return &cobra.Command{
		Use:           "sync <repo-ref>",
		Short:         "Refresh Shiva catalog data",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			selector, err := parseRepoSnapshotSelector(args[0], *flags, false, false)
			if err != nil {
				return err
			}
			if err := validateNoCallInputFlags(*flags, "sync"); err != nil {
				return err
			}

			service, err := loadService(serviceFactory)
			if err != nil {
				return err
			}

			body, err := executeSyncCommand(cmd.Context(), service, selector, *flags)
			if err != nil {
				return err
			}
			return writeOutput(cmd.OutOrStdout(), body)
		},
	}
}
