package cli

import "strings"

type Selector struct {
	RepoPath    string
	OperationID string
}

func (s Selector) HasOperation() bool {
	return strings.TrimSpace(s.OperationID) != ""
}

func ParseSelector(raw string) (Selector, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return Selector{}, &InvalidInputError{Message: "selector must not be empty"}
	}

	repoPath, operationID, hasOperation := strings.Cut(value, "#")
	repoPath = strings.TrimSpace(repoPath)
	operationID = strings.TrimSpace(operationID)

	if repoPath == "" {
		return Selector{}, &InvalidInputError{Message: "repo path must not be empty"}
	}
	if !hasOperation {
		return Selector{RepoPath: repoPath}, nil
	}
	if operationID == "" {
		return Selector{}, &InvalidInputError{Message: "operation id must not be empty"}
	}

	return Selector{
		RepoPath:    repoPath,
		OperationID: operationID,
	}, nil
}
