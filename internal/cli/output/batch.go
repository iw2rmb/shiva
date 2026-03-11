package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/iw2rmb/shiva/internal/cli/request"
)

type BatchItem struct {
	Index   int              `json:"index"`
	Request request.Envelope `json:"request"`
	OK      bool             `json:"ok"`
	Output  *BatchPayload    `json:"output,omitempty"`
	Error   string           `json:"error,omitempty"`
}

type BatchPayload struct {
	Format  string       `json:"format"`
	Payload *payloadView `json:"payload,omitempty"`
}

func NewBatchPayload(format string, body []byte) *BatchPayload {
	return &BatchPayload{
		Format:  format,
		Payload: payloadFromBytes(body),
	}
}

func EncodeBatchItemNDJSON(writer io.Writer, item BatchItem) error {
	if writer == nil {
		return fmt.Errorf("batch writer is not configured")
	}
	return json.NewEncoder(writer).Encode(item)
}

func RenderBatchItemsJSON(items []BatchItem) ([]byte, error) {
	return json.Marshal(items)
}

func RenderBatchItemsNDJSON(items []BatchItem) ([]byte, error) {
	if len(items) == 0 {
		return nil, nil
	}

	buffer := &bytes.Buffer{}
	for _, item := range items {
		if err := EncodeBatchItemNDJSON(buffer, item); err != nil {
			return nil, err
		}
	}
	return buffer.Bytes(), nil
}
