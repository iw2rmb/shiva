package cli

import (
	"strings"

	"github.com/iw2rmb/shiva/internal/cli/request"
	"github.com/iw2rmb/shiva/internal/repoid"
)

type PackedSelector struct {
	Namespace   string
	Repo        string
	Target      string
	OperationID string
}

func (s PackedSelector) RepoPath() string {
	return repoid.Identity{Namespace: s.Namespace, Repo: s.Repo}.Path()
}

func (s PackedSelector) HasTarget() bool {
	return strings.TrimSpace(s.Target) != ""
}

func (s PackedSelector) HasOperation() bool {
	return strings.TrimSpace(s.OperationID) != ""
}

type ShorthandInvocation struct {
	Envelope request.Envelope
}

func ParsePackedSelector(raw string) (PackedSelector, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return PackedSelector{}, &InvalidInputError{Message: "selector must not be empty"}
	}

	repoAndTarget, operationID, hasOperation := strings.Cut(value, "#")
	repoPath, target, hasTarget := strings.Cut(strings.TrimSpace(repoAndTarget), "@")

	repoPath = strings.TrimSpace(repoPath)
	target = strings.TrimSpace(target)
	operationID = strings.TrimSpace(operationID)

	if repoPath == "" {
		return PackedSelector{}, &InvalidInputError{Message: "repo path must not be empty"}
	}
	if hasTarget && target == "" {
		return PackedSelector{}, &InvalidInputError{Message: "target must not be empty"}
	}
	if hasOperation && operationID == "" {
		return PackedSelector{}, &InvalidInputError{Message: "operation id must not be empty"}
	}

	identity, err := repoid.ParsePath(repoPath)
	if err != nil {
		return PackedSelector{}, &InvalidInputError{Message: err.Error()}
	}

	return PackedSelector{
		Namespace:   identity.Namespace,
		Repo:        identity.Repo,
		Target:      target,
		OperationID: operationID,
	}, nil
}

func ParseShorthandInvocation(args []string, flags RootFlags) (ShorthandInvocation, error) {
	if len(args) == 0 {
		return ShorthandInvocation{}, &InvalidInputError{Message: "expected a selector or subcommand"}
	}

	packed, err := ParsePackedSelector(args[0])
	if err != nil {
		return ShorthandInvocation{}, err
	}

	target, err := mergeTargets(packed.Target, flags.Target)
	if err != nil {
		return ShorthandInvocation{}, err
	}

	envelope := request.Envelope{
		Namespace:  packed.Namespace,
		Repo:       packed.Repo,
		API:        flags.API,
		RevisionID: flags.RevisionID,
		SHA:        flags.SHA,
		DryRun:     flags.DryRun,
	}

	switch len(args) {
	case 1:
		switch {
		case packed.HasOperation():
			envelope.OperationID = packed.OperationID
			if target != "" {
				envelope.Kind = request.KindCall
				envelope.Target = target
				normalized, normalizeErr := request.NormalizeCallEnvelope(envelope, request.NormalizeCallOptions{
					DefaultTarget:    target,
					AllowMissingKind: true,
				})
				if normalizeErr != nil {
					return ShorthandInvocation{}, normalizeCLIValidation(normalizeErr)
				}
				return ShorthandInvocation{Envelope: normalized}, nil
			}

			if flags.DryRun {
				return ShorthandInvocation{}, &InvalidInputError{Message: "--dry-run requires call mode"}
			}

			normalized, normalizeErr := request.NormalizeEnvelope(envelope, request.NormalizeOptions{
				DefaultKind:      request.KindOperation,
				AllowMissingKind: true,
			})
			if normalizeErr != nil {
				return ShorthandInvocation{}, normalizeCLIValidation(normalizeErr)
			}
			return ShorthandInvocation{Envelope: normalized}, nil
		case target != "":
			return ShorthandInvocation{}, &InvalidInputError{Message: "call mode requires either #<operation-id> or <method> <path>"}
		default:
			if flags.DryRun {
				return ShorthandInvocation{}, &InvalidInputError{Message: "--dry-run requires call mode"}
			}
			normalized, normalizeErr := request.NormalizeEnvelope(envelope, request.NormalizeOptions{
				DefaultKind:      request.KindSpec,
				AllowMissingKind: true,
			})
			if normalizeErr != nil {
				return ShorthandInvocation{}, normalizeCLIValidation(normalizeErr)
			}
			return ShorthandInvocation{Envelope: normalized}, nil
		}
	case 3:
		if packed.HasOperation() {
			return ShorthandInvocation{}, &InvalidInputError{Message: "packed operation selectors do not accept an additional method and path"}
		}

		envelope.Method = args[1]
		envelope.Path = args[2]
		if target != "" {
			envelope.Kind = request.KindCall
			envelope.Target = target
			normalized, normalizeErr := request.NormalizeCallEnvelope(envelope, request.NormalizeCallOptions{
				DefaultTarget:    target,
				AllowMissingKind: true,
			})
			if normalizeErr != nil {
				return ShorthandInvocation{}, normalizeCLIValidation(normalizeErr)
			}
			return ShorthandInvocation{Envelope: normalized}, nil
		}

		if flags.DryRun {
			return ShorthandInvocation{}, &InvalidInputError{Message: "--dry-run requires call mode"}
		}

		normalized, normalizeErr := request.NormalizeEnvelope(envelope, request.NormalizeOptions{
			DefaultKind:      request.KindOperation,
			AllowMissingKind: true,
		})
		if normalizeErr != nil {
			return ShorthandInvocation{}, normalizeCLIValidation(normalizeErr)
		}
		return ShorthandInvocation{Envelope: normalized}, nil
	default:
		return ShorthandInvocation{}, &InvalidInputError{Message: "expected either <repo-ref>, <repo-ref>#<operation-id>, or <repo-ref> <method> <path>"}
	}
}

func mergeTargets(packed string, flag string) (string, error) {
	packed = strings.TrimSpace(packed)
	flag = strings.TrimSpace(flag)

	switch {
	case packed == "":
		return flag, nil
	case flag == "":
		return packed, nil
	case packed == flag:
		return packed, nil
	default:
		return "", &InvalidInputError{Message: "packed @target must match --via"}
	}
}

func normalizeCLIValidation(err error) error {
	if err == nil {
		return nil
	}

	if validationErr, ok := err.(*request.ValidationError); ok {
		return &InvalidInputError{Message: validationErr.Error()}
	}
	return err
}
