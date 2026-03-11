package openapi

import (
	"encoding/json"
	"fmt"
)

// ExtractEndpointsFromSpecJSON applies the canonical endpoint extraction rules
// to a stored canonical spec body.
func ExtractEndpointsFromSpecJSON(raw []byte) ([]Endpoint, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("%w: spec json must not be empty", ErrInvalidOpenAPIDocument)
	}

	var document any
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, fmt.Errorf("unmarshal canonical spec json: %w", err)
	}

	return extractEndpoints(document)
}
