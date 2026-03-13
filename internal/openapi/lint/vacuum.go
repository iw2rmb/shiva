package lint

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sort"
	"strings"
	"time"

	vacuumModel "github.com/daveshanley/vacuum/model"
	"github.com/daveshanley/vacuum/motor"
	"github.com/daveshanley/vacuum/rulesets"
)

const (
	defaultRuleTimeout       = 5 * time.Second
	defaultNodeLookupTimeout = 5 * time.Second
)

type CanonicalIssue struct {
	RuleID   string
	Message  string
	JSONPath string
	RangePos [4]int32
}

type CanonicalFailure struct {
	Message string
}

type CanonicalExecutionResult struct {
	Issues  []CanonicalIssue
	Failure *CanonicalFailure
}

type CanonicalRunner struct {
	ruleset           *rulesets.RuleSet
	logger            *slog.Logger
	timeout           time.Duration
	nodeLookupTimeout time.Duration
}

var defaultCanonicalRunner = NewCanonicalRunner()

func DefaultCanonicalRunner() *CanonicalRunner {
	return defaultCanonicalRunner
}

func NewCanonicalRunner() *CanonicalRunner {
	silentLogger := slog.New(slog.NewTextHandler(io.Discard, nil))
	defaultRuleSets := rulesets.BuildDefaultRuleSetsWithLogger(silentLogger)

	return &CanonicalRunner{
		ruleset:           defaultRuleSets.GenerateOpenAPIDefaultRuleSet(),
		logger:            silentLogger,
		timeout:           defaultRuleTimeout,
		nodeLookupTimeout: defaultNodeLookupTimeout,
	}
}

func (r *CanonicalRunner) RunCanonicalSpec(
	ctx context.Context,
	specYAML string,
) (CanonicalExecutionResult, error) {
	if err := ctx.Err(); err != nil {
		return CanonicalExecutionResult{}, err
	}

	specYAML = strings.TrimSpace(specYAML)
	if specYAML == "" {
		return CanonicalExecutionResult{}, errors.New("canonical spec yaml must not be empty")
	}

	execution := &motor.RuleSetExecution{
		RuleSet:           r.ruleset,
		SpecFileName:      "canonical-openapi.yaml",
		Spec:              []byte(specYAML + "\n"),
		Base:              ".",
		AllowLookup:       false,
		SilenceLogs:       true,
		Logger:            r.logger,
		Timeout:           r.timeout,
		NodeLookupTimeout: r.nodeLookupTimeout,
	}

	result := motor.ApplyRulesToRuleSet(execution)
	if err := ctx.Err(); err != nil {
		return CanonicalExecutionResult{}, err
	}

	if failure := normalizeCanonicalFailure(result.Errors); failure != nil {
		return CanonicalExecutionResult{Failure: failure}, nil
	}

	return CanonicalExecutionResult{
		Issues: normalizeCanonicalIssues(result.Results),
	}, nil
}

func normalizeCanonicalFailure(errorsList []error) *CanonicalFailure {
	if len(errorsList) == 0 {
		return nil
	}

	messages := make([]string, 0, len(errorsList))
	seen := make(map[string]struct{}, len(errorsList))
	for _, err := range errorsList {
		message := strings.TrimSpace(err.Error())
		if message == "" {
			continue
		}
		if _, exists := seen[message]; exists {
			continue
		}
		seen[message] = struct{}{}
		messages = append(messages, message)
	}
	if len(messages) == 0 {
		return &CanonicalFailure{Message: "vacuum validation failed"}
	}
	return &CanonicalFailure{Message: strings.Join(messages, "; ")}
}

func normalizeCanonicalIssues(results []vacuumModel.RuleFunctionResult) []CanonicalIssue {
	issues := make([]CanonicalIssue, 0, len(results))
	for _, result := range results {
		issues = append(issues, CanonicalIssue{
			RuleID:   strings.TrimSpace(result.RuleId),
			Message:  strings.TrimSpace(result.Message),
			JSONPath: strings.TrimSpace(result.Path),
			RangePos: [4]int32{
				int32(result.Range.Start.Line),
				int32(result.Range.Start.Char),
				int32(result.Range.End.Line),
				int32(result.Range.End.Char),
			},
		})
	}

	sort.SliceStable(issues, func(i, j int) bool {
		if issues[i].RangePos != issues[j].RangePos {
			for idx := range issues[i].RangePos {
				if issues[i].RangePos[idx] == issues[j].RangePos[idx] {
					continue
				}
				return issues[i].RangePos[idx] < issues[j].RangePos[idx]
			}
		}
		if issues[i].RuleID == issues[j].RuleID {
			return issues[i].JSONPath < issues[j].JSONPath
		}
		return issues[i].RuleID < issues[j].RuleID
	})

	return issues
}
