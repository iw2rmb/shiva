package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Health struct {
	Status  string `json:"status"`
	Details string `json:"details,omitempty"`
}

type Store struct {
	pool       *pgxpool.Pool
	configured bool
}

func New(ctx context.Context, databaseURL string) (*Store, error) {
	databaseURL = strings.TrimSpace(databaseURL)
	if databaseURL == "" {
		return nil, errors.New("SHIVA_DATABASE_URL must not be empty")
	}

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("create database pool: %w", err)
	}

	return &Store{pool: pool, configured: true}, nil
}

func (s *Store) Close() {
	if s == nil {
		return
	}
	if s.pool != nil {
		s.pool.Close()
	}
}

func (s *Store) Health(ctx context.Context) Health {
	if s == nil || !s.configured || s.pool == nil {
		return Health{Status: "unreachable", Details: "database connection is not configured"}
	}

	testCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()

	if err := s.pool.Ping(testCtx); err != nil {
		return Health{Status: "unreachable", Details: err.Error()}
	}

	return Health{Status: "ok"}
}
