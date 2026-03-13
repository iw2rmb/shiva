package httpserver

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/gofiber/fiber/v2"

	"github.com/iw2rmb/shiva/internal/repoid"
	"github.com/iw2rmb/shiva/internal/store"
)

type gitlabCIValidator interface {
	ValidateGitLabCI(ctx context.Context, input GitLabCIValidationInput) (GitLabCIValidationResult, error)
}

type GitLabCIValidationFormat string

const (
	GitLabCIValidationFormatShiva             GitLabCIValidationFormat = "shiva"
	GitLabCIValidationFormatGitLabCodeQuality GitLabCIValidationFormat = "gitlab_code_quality"
)

type GitLabCIValidationInput struct {
	GitLabProjectID int64
	Namespace       string
	Repo            string
	SHA             string
	Branch          string
	ParentSHA       string
	Format          GitLabCIValidationFormat
}

type GitLabCIValidationResult struct {
	Specs []GitLabCIValidationSpecResult
}

type GitLabCIValidationSpecResult struct {
	RootPath string
	Issues   []GitLabCIValidationIssue
}

type GitLabCIValidationIssue struct {
	RuleID   string
	Severity string
	Message  string
	JSONPath string
	FilePath string
	RangePos [4]int32
}

type gitlabCIValidationRequest struct {
	GitLabProjectID int64  `json:"gitlab_project_id"`
	Namespace       string `json:"namespace"`
	Repo            string `json:"repo"`
	SHA             string `json:"sha"`
	Branch          string `json:"branch"`
	ParentSHA       string `json:"parent_sha"`
	Format          string `json:"format"`
}

type gitlabCIValidationShivaResponse struct {
	Status string                           `json:"status"`
	Format GitLabCIValidationFormat         `json:"format"`
	Repo   gitlabCIValidationRepoResponse   `json:"repo"`
	Specs  []gitlabCIValidationSpecResponse `json:"specs"`
}

type gitlabCIValidationRepoResponse struct {
	GitLabProjectID int64  `json:"gitlab_project_id"`
	Namespace       string `json:"namespace"`
	Repo            string `json:"repo"`
	SHA             string `json:"sha"`
	Branch          string `json:"branch"`
	ParentSHA       string `json:"parent_sha,omitempty"`
}

type gitlabCIValidationSpecResponse struct {
	RootPath string                            `json:"root_path"`
	Issues   []gitlabCIValidationIssueResponse `json:"issues"`
}

type gitlabCIValidationIssueResponse struct {
	RuleID   string   `json:"rule_id"`
	Severity string   `json:"severity"`
	Message  string   `json:"message"`
	JSONPath string   `json:"json_path,omitempty"`
	FilePath string   `json:"file_path"`
	RangePos [4]int32 `json:"range"`
}

type gitlabCodeQualityIssueResponse struct {
	Description string                    `json:"description"`
	CheckName   string                    `json:"check_name"`
	Fingerprint string                    `json:"fingerprint"`
	Severity    string                    `json:"severity"`
	Location    gitlabCodeQualityLocation `json:"location"`
}

type gitlabCodeQualityLocation struct {
	Path  string                 `json:"path"`
	Lines gitlabCodeQualityLines `json:"lines"`
}

type gitlabCodeQualityLines struct {
	Begin int `json:"begin"`
	End   int `json:"end,omitempty"`
}

type normalizedGitLabCIValidationSpec struct {
	rootPath string
	issues   []normalizedGitLabCIValidationIssue
}

type normalizedGitLabCIValidationIssue struct {
	ruleID   string
	severity string
	message  string
	jsonPath string
	filePath string
	rangePos [4]int32
}

func (s *Server) handleGitLabCIValidate(c *fiber.Ctx) error {
	body := c.Body()
	if len(body) == 0 || !json.Valid(body) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "request body must be valid JSON",
		})
	}

	var request gitlabCIValidationRequest
	if err := json.Unmarshal(body, &request); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "failed to parse GitLab CI validation payload",
		})
	}

	input, err := normalizeGitLabCIValidationInput(request)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	if s.gitlabCIValidator == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"error": "gitlab ci validator is not configured",
		})
	}

	ctx := c.UserContext()
	if ctx == nil {
		ctx = c.Context()
	}

	result, err := s.gitlabCIValidator.ValidateGitLabCI(ctx, input)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrStoreNotConfigured):
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
				"error": "database is not configured",
			})
		default:
			if s.logger != nil {
				s.logger.Error(
					"gitlab ci validation failed",
					"gitlab_project_id", input.GitLabProjectID,
					"namespace", input.Namespace,
					"repo", input.Repo,
					"sha", input.SHA,
					"error", err,
				)
			}
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "gitlab ci validation failed",
			})
		}
	}

	switch input.Format {
	case GitLabCIValidationFormatGitLabCodeQuality:
		return writeGitLabCICodeQualityResponse(c, input, result)
	default:
		return writeGitLabCIShivaResponse(c, input, result)
	}
}

func normalizeGitLabCIValidationInput(request gitlabCIValidationRequest) (GitLabCIValidationInput, error) {
	identity, err := repoid.Normalize(request.Namespace, request.Repo)
	if err != nil {
		return GitLabCIValidationInput{}, err
	}

	format, err := parseGitLabCIValidationFormat(request.Format)
	if err != nil {
		return GitLabCIValidationInput{}, err
	}

	input := GitLabCIValidationInput{
		GitLabProjectID: request.GitLabProjectID,
		Namespace:       identity.Namespace,
		Repo:            identity.Repo,
		SHA:             strings.TrimSpace(request.SHA),
		Branch:          strings.TrimSpace(request.Branch),
		ParentSHA:       strings.TrimSpace(request.ParentSHA),
		Format:          format,
	}

	switch {
	case input.GitLabProjectID <= 0:
		return GitLabCIValidationInput{}, errors.New("gitlab_project_id must be positive")
	case input.SHA == "":
		return GitLabCIValidationInput{}, errors.New("sha must not be empty")
	case input.Branch == "":
		return GitLabCIValidationInput{}, errors.New("branch must not be empty")
	default:
		return input, nil
	}
}

func parseGitLabCIValidationFormat(value string) (GitLabCIValidationFormat, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", string(GitLabCIValidationFormatShiva):
		return GitLabCIValidationFormatShiva, nil
	case string(GitLabCIValidationFormatGitLabCodeQuality):
		return GitLabCIValidationFormatGitLabCodeQuality, nil
	default:
		return "", errors.New(`format must be one of "shiva" or "gitlab_code_quality"`)
	}
}

func writeGitLabCIShivaResponse(
	c *fiber.Ctx,
	input GitLabCIValidationInput,
	result GitLabCIValidationResult,
) error {
	specs := normalizeGitLabCIValidationSpecs(result.Specs)
	response := gitlabCIValidationShivaResponse{
		Status: "ok",
		Format: GitLabCIValidationFormatShiva,
		Repo: gitlabCIValidationRepoResponse{
			GitLabProjectID: input.GitLabProjectID,
			Namespace:       input.Namespace,
			Repo:            input.Repo,
			SHA:             input.SHA,
			Branch:          input.Branch,
			ParentSHA:       input.ParentSHA,
		},
		Specs: make([]gitlabCIValidationSpecResponse, 0, len(specs)),
	}

	for _, spec := range specs {
		row := gitlabCIValidationSpecResponse{
			RootPath: spec.rootPath,
			Issues:   make([]gitlabCIValidationIssueResponse, 0, len(spec.issues)),
		}
		for _, issue := range spec.issues {
			row.Issues = append(row.Issues, gitlabCIValidationIssueResponse{
				RuleID:   issue.ruleID,
				Severity: issue.severity,
				Message:  issue.message,
				JSONPath: issue.jsonPath,
				FilePath: issue.filePath,
				RangePos: issue.rangePos,
			})
		}
		response.Specs = append(response.Specs, row)
	}

	return c.Status(fiber.StatusOK).JSON(response)
}

func writeGitLabCICodeQualityResponse(
	c *fiber.Ctx,
	input GitLabCIValidationInput,
	result GitLabCIValidationResult,
) error {
	specs := normalizeGitLabCIValidationSpecs(result.Specs)
	issues := make([]gitlabCodeQualityIssueResponse, 0)
	for _, spec := range specs {
		for _, issue := range spec.issues {
			beginLine, endLine := gitlabCodeQualityLineRange(issue.rangePos)
			row := gitlabCodeQualityIssueResponse{
				Description: issue.message,
				CheckName:   issue.ruleID,
				Fingerprint: gitlabCodeQualityFingerprint(input, spec.rootPath, issue),
				Severity:    normalizeGitLabCodeQualitySeverity(issue.severity),
				Location: gitlabCodeQualityLocation{
					Path: issue.filePath,
					Lines: gitlabCodeQualityLines{
						Begin: beginLine,
					},
				},
			}
			if endLine > beginLine {
				row.Location.Lines.End = endLine
			}
			issues = append(issues, row)
		}
	}

	return c.Status(fiber.StatusOK).JSON(issues)
}

func normalizeGitLabCIValidationSpecs(specs []GitLabCIValidationSpecResult) []normalizedGitLabCIValidationSpec {
	normalized := make([]normalizedGitLabCIValidationSpec, 0, len(specs))
	for _, spec := range specs {
		row := normalizedGitLabCIValidationSpec{
			rootPath: strings.TrimSpace(spec.RootPath),
			issues:   normalizeGitLabCIValidationIssues(spec.Issues),
		}
		if row.rootPath == "" {
			continue
		}
		normalized = append(normalized, row)
	}

	sortGitLabCIValidationSpecs(normalized)
	return normalized
}

func normalizeGitLabCIValidationIssues(issues []GitLabCIValidationIssue) []normalizedGitLabCIValidationIssue {
	normalized := make([]normalizedGitLabCIValidationIssue, 0, len(issues))
	for _, issue := range issues {
		row := normalizedGitLabCIValidationIssue{
			ruleID:   strings.TrimSpace(issue.RuleID),
			severity: normalizeGitLabCIValidationSeverity(issue.Severity),
			message:  strings.TrimSpace(issue.Message),
			jsonPath: strings.TrimSpace(issue.JSONPath),
			filePath: strings.TrimSpace(issue.FilePath),
			rangePos: issue.RangePos,
		}
		if row.ruleID == "" || row.message == "" || row.filePath == "" {
			continue
		}
		normalized = append(normalized, row)
	}

	sortGitLabCIValidationIssues(normalized)
	return normalized
}

func normalizeGitLabCIValidationSeverity(value string) string {
	severity := strings.ToLower(strings.TrimSpace(value))
	if severity == "" {
		return "warning"
	}
	return severity
}

func normalizeGitLabCodeQualitySeverity(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "blocker":
		return "blocker"
	case "critical":
		return "critical"
	case "major", "error":
		return "major"
	case "info", "information":
		return "info"
	default:
		return "minor"
	}
}

func gitlabCodeQualityLineRange(rangePos [4]int32) (int, int) {
	beginLine := int(rangePos[0])
	if beginLine < 1 {
		beginLine = 1
	}

	endLine := int(rangePos[2])
	if endLine < beginLine {
		endLine = beginLine
	}
	return beginLine, endLine
}

func gitlabCodeQualityFingerprint(
	input GitLabCIValidationInput,
	rootPath string,
	issue normalizedGitLabCIValidationIssue,
) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf(
		"%s/%s|%s|%s|%s|%d:%d:%d:%d|%s",
		input.Namespace,
		input.Repo,
		strings.TrimSpace(rootPath),
		issue.ruleID,
		issue.jsonPath,
		issue.rangePos[0],
		issue.rangePos[1],
		issue.rangePos[2],
		issue.rangePos[3],
		issue.message,
	)))
	return fmt.Sprintf("%x", sum)
}

func sortGitLabCIValidationSpecs(specs []normalizedGitLabCIValidationSpec) {
	sort.SliceStable(specs, func(i, j int) bool {
		return specs[i].rootPath < specs[j].rootPath
	})
}

func sortGitLabCIValidationIssues(issues []normalizedGitLabCIValidationIssue) {
	sort.SliceStable(issues, func(i, j int) bool {
		return gitlabCIValidationIssueLess(issues[i], issues[j])
	})
}

func gitlabCIValidationIssueLess(left, right normalizedGitLabCIValidationIssue) bool {
	if left.filePath != right.filePath {
		return left.filePath < right.filePath
	}
	if left.rangePos != right.rangePos {
		for idx := range left.rangePos {
			if left.rangePos[idx] == right.rangePos[idx] {
				continue
			}
			return left.rangePos[idx] < right.rangePos[idx]
		}
	}
	if left.ruleID != right.ruleID {
		return left.ruleID < right.ruleID
	}
	if left.jsonPath != right.jsonPath {
		return left.jsonPath < right.jsonPath
	}
	return left.message < right.message
}
