package httpserver

import (
	"context"
	"fmt"
	"sync"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/iw2rmb/shiva/internal/store"
)

type runtimeSpecCache struct {
	mu   sync.RWMutex
	docs map[int64]*openapi3.T
}

func newRuntimeSpecCache() *runtimeSpecCache {
	return &runtimeSpecCache{
		docs: make(map[int64]*openapi3.T),
	}
}

func (c *runtimeSpecCache) GetOrLoad(
	ctx context.Context,
	apiSpecRevisionID int64,
	load func(context.Context, int64) (store.SpecArtifact, error),
) (*openapi3.T, error) {
	if c == nil {
		return nil, fmt.Errorf("runtime spec cache is not configured")
	}
	if apiSpecRevisionID < 1 {
		return nil, fmt.Errorf("api spec revision id must be positive")
	}

	c.mu.RLock()
	document := c.docs[apiSpecRevisionID]
	c.mu.RUnlock()
	if document != nil {
		return document, nil
	}

	artifact, err := load(ctx, apiSpecRevisionID)
	if err != nil {
		return nil, err
	}

	loader := openapi3.NewLoader()
	document, err = loader.LoadFromData(artifact.SpecJSON)
	if err != nil {
		return nil, fmt.Errorf("parse runtime spec for api_spec_revision_id=%d: %w", apiSpecRevisionID, err)
	}
	if err := document.Validate(ctx); err != nil {
		return nil, fmt.Errorf("validate runtime spec for api_spec_revision_id=%d: %w", apiSpecRevisionID, err)
	}

	c.mu.Lock()
	if cached := c.docs[apiSpecRevisionID]; cached != nil {
		c.mu.Unlock()
		return cached, nil
	}
	c.docs[apiSpecRevisionID] = document
	c.mu.Unlock()

	return document, nil
}
