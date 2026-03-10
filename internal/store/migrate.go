package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	shivaschema "github.com/iw2rmb/shiva/sql/schema"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const currentSchemaVersion = "000001_initial"

const createSchemaMigrationsTableSQL = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version TEXT PRIMARY KEY,
    checksum TEXT NOT NULL,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
`

const loadSchemaMigrationChecksumSQL = `
SELECT checksum
FROM schema_migrations
WHERE version = $1;
`

const countExistingUserTablesSQL = `
SELECT COUNT(*)::BIGINT
FROM pg_catalog.pg_tables
WHERE schemaname = ANY (current_schemas(false))
  AND tablename <> 'schema_migrations';
`

const insertSchemaMigrationSQL = `
INSERT INTO schema_migrations (version, checksum)
VALUES ($1, $2)
ON CONFLICT (version) DO NOTHING;
`

type migrationDB interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, arguments ...any) pgx.Row
}

type migrationTx interface {
	migrationDB
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

type migrationPool interface {
	migrationDB
	Begin(ctx context.Context) (pgx.Tx, error)
}

func applyCurrentSchema(ctx context.Context, db migrationPool) error {
	if _, err := db.Exec(ctx, createSchemaMigrationsTableSQL); err != nil {
		return fmt.Errorf("create schema migrations table: %w", err)
	}

	schemaSQL := shivaschema.InitialSchemaSQL()
	checksum := schemaChecksum(schemaSQL)

	recordedChecksum, err := loadSchemaMigrationChecksum(ctx, db, currentSchemaVersion)
	switch {
	case err == nil:
		if recordedChecksum != checksum {
			return fmt.Errorf(
				"schema migration %s checksum mismatch: recorded=%s current=%s",
				currentSchemaVersion,
				recordedChecksum,
				checksum,
			)
		}
		return nil
	case !errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("load schema migration %s: %w", currentSchemaVersion, err)
	}

	existingTableCount, err := countExistingUserTables(ctx, db)
	if err != nil {
		return fmt.Errorf("count existing user tables before applying %s: %w", currentSchemaVersion, err)
	}
	if existingTableCount > 0 {
		return fmt.Errorf(
			"schema migration %s is not recorded but database already has %d user tables",
			currentSchemaVersion,
			existingTableCount,
		)
	}

	tx, err := beginMigrationTx(ctx, db)
	if err != nil {
		return fmt.Errorf("begin schema migration %s transaction: %w", currentSchemaVersion, err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if err := execMigrationSQL(ctx, tx, schemaSQL); err != nil {
		return fmt.Errorf("apply schema migration %s: %w", currentSchemaVersion, err)
	}
	if _, err := tx.Exec(ctx, insertSchemaMigrationSQL, currentSchemaVersion, checksum); err != nil {
		return fmt.Errorf("record schema migration %s: %w", currentSchemaVersion, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit schema migration %s: %w", currentSchemaVersion, err)
	}

	recordedChecksum, err = loadSchemaMigrationChecksum(ctx, db, currentSchemaVersion)
	if err != nil {
		return fmt.Errorf("load schema migration %s after apply: %w", currentSchemaVersion, err)
	}
	if recordedChecksum != checksum {
		return fmt.Errorf(
			"schema migration %s checksum mismatch after apply: recorded=%s current=%s",
			currentSchemaVersion,
			recordedChecksum,
			checksum,
		)
	}
	return nil
}

func loadSchemaMigrationChecksum(ctx context.Context, db migrationDB, version string) (string, error) {
	var checksum string
	if err := db.QueryRow(ctx, loadSchemaMigrationChecksumSQL, version).Scan(&checksum); err != nil {
		return "", err
	}
	return checksum, nil
}

func countExistingUserTables(ctx context.Context, db migrationDB) (int64, error) {
	var count int64
	if err := db.QueryRow(ctx, countExistingUserTablesSQL).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func schemaChecksum(schemaSQL string) string {
	sum := sha256.Sum256([]byte(schemaSQL))
	return hex.EncodeToString(sum[:])
}

func beginMigrationTx(ctx context.Context, db migrationPool) (migrationTx, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	typed, ok := tx.(migrationTx)
	if !ok {
		return nil, errors.New("migration transaction does not implement required interface")
	}
	return typed, nil
}

func execMigrationSQL(ctx context.Context, tx migrationTx, sql string) error {
	statements := splitSQLStatements(sql)
	for _, statement := range statements {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		if _, err := tx.Exec(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

// splitSQLStatements splits a SQL script by semicolons while ignoring semicolons
// inside quoted strings, dollar-quoted strings, and comments.
func splitSQLStatements(script string) []string {
	var statements []string
	var builder strings.Builder

	inSingle := false
	inDouble := false
	inLineComment := false
	inBlockComment := false
	inDollar := false
	var dollarTag string

	flush := func() {
		statement := strings.TrimSpace(builder.String())
		if statement != "" {
			statements = append(statements, statement)
		}
		builder.Reset()
	}

	runes := []rune(script)
	for i := 0; i < len(runes); i++ {
		r := runes[i]

		if inLineComment {
			builder.WriteRune(r)
			if r == '\n' {
				inLineComment = false
			}
			continue
		}

		if inBlockComment {
			builder.WriteRune(r)
			if r == '*' && i+1 < len(runes) && runes[i+1] == '/' {
				builder.WriteRune('/')
				i++
				inBlockComment = false
			}
			continue
		}

		if inDollar {
			builder.WriteRune(r)
			if r == '$' {
				if dollarTag == "" {
					if i+1 < len(runes) && runes[i+1] == '$' {
						builder.WriteRune('$')
						i++
						inDollar = false
					}
				} else {
					tagRunes := []rune(dollarTag)
					tagLen := len(tagRunes)
					if i+1+tagLen < len(runes) {
						match := true
						for j := 0; j < tagLen; j++ {
							if runes[i+1+j] != tagRunes[j] {
								match = false
								break
							}
						}
						if match && runes[i+1+tagLen] == '$' {
							for j := 0; j < tagLen+1; j++ {
								builder.WriteRune(runes[i+1+j])
							}
							i += tagLen + 1
							inDollar = false
							dollarTag = ""
						}
					}
				}
			}
			continue
		}

		switch r {
		case '\'', '"':
			builder.WriteRune(r)
			if r == '\'' && !inDouble {
				if inSingle {
					if i+1 < len(runes) && runes[i+1] == '\'' {
						builder.WriteRune('\'')
						i++
					} else {
						inSingle = false
					}
				} else {
					inSingle = true
				}
				continue
			}
			if r == '"' && !inSingle {
				inDouble = !inDouble
				continue
			}
		case '-':
			if !inSingle && !inDouble && i+1 < len(runes) && runes[i+1] == '-' {
				builder.WriteString("--")
				i++
				inLineComment = true
				continue
			}
			builder.WriteRune(r)
			continue
		case '/':
			if !inSingle && !inDouble && i+1 < len(runes) && runes[i+1] == '*' {
				builder.WriteString("/*")
				i++
				inBlockComment = true
				continue
			}
			builder.WriteRune(r)
			continue
		case '$':
			if !inSingle && !inDouble {
				if i+1 < len(runes) && runes[i+1] == '$' {
					builder.WriteString("$$")
					i++
					inDollar = true
					dollarTag = ""
					continue
				}
				j := i + 1
				var tagBuilder strings.Builder
				for j < len(runes) && ((runes[j] >= 'a' && runes[j] <= 'z') || (runes[j] >= 'A' && runes[j] <= 'Z') || (runes[j] >= '0' && runes[j] <= '9') || runes[j] == '_') {
					tagBuilder.WriteRune(runes[j])
					j++
				}
				if j < len(runes) && runes[j] == '$' {
					builder.WriteRune('$')
					for k := i + 1; k <= j; k++ {
						builder.WriteRune(runes[k])
					}
					i = j
					inDollar = true
					dollarTag = tagBuilder.String()
					continue
				}
			}
			builder.WriteRune(r)
			continue
		case ';':
			if !inSingle && !inDouble {
				flush()
				continue
			}
			builder.WriteRune(r)
			continue
		}

		builder.WriteRune(r)
	}

	flush()
	return statements
}
