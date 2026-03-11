package output

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/iw2rmb/shiva/internal/cli/request"
)

func RenderRequestEnvelopesNDJSON(envelopes []request.Envelope) ([]byte, error) {
	if len(envelopes) == 0 {
		return nil, nil
	}

	buffer := &bytes.Buffer{}
	encoder := json.NewEncoder(buffer)
	for _, envelope := range envelopes {
		if err := encoder.Encode(envelope); err != nil {
			return nil, fmt.Errorf("encode request envelope: %w", err)
		}
	}
	return buffer.Bytes(), nil
}
