package store

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	shivaschema "github.com/iw2rmb/shiva/sql/schema"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestApplyCurrentSchema_AppliesMissingSchema(t *testing.T) {
	t.Parallel()

	db := &fakeMigrationDB{
		queryResults: []fakeMigrationQueryResult{
			{err: pgx.ErrNoRows},
			{count: 0},
			{checksum: schemaChecksum(currentSchemaSQLForTest())},
		},
	}

	if err := applyCurrentSchema(context.Background(), db); err != nil {
		t.Fatalf("applyCurrentSchema() unexpected error: %v", err)
	}

	if len(db.execCalls) != 1 {
		t.Fatalf("expected 1 non-transactional exec call, got %d", len(db.execCalls))
	}
	if strings.TrimSpace(db.execCalls[0].sql) != strings.TrimSpace(createSchemaMigrationsTableSQL) {
		t.Fatalf("expected first exec to create schema_migrations table")
	}
	if db.tx == nil {
		t.Fatal("expected migration transaction to be created")
	}
	if len(db.tx.execCalls) == 0 {
		t.Fatal("expected transactional schema statements to be executed")
	}
	lastExec := db.tx.execCalls[len(db.tx.execCalls)-1]
	if strings.TrimSpace(lastExec.sql) != strings.TrimSpace(insertSchemaMigrationSQL) {
		t.Fatalf("expected final transactional exec to record schema migration, got %q", lastExec.sql)
	}
	if !reflect.DeepEqual(lastExec.arguments, []any{currentSchemaVersion, schemaChecksum(currentSchemaSQLForTest())}) {
		t.Fatalf("unexpected insert arguments: %#v", lastExec.arguments)
	}
	if !db.tx.committed {
		t.Fatal("expected migration transaction to commit")
	}
}

func TestApplyCurrentSchema_SkipsWhenChecksumMatches(t *testing.T) {
	t.Parallel()

	db := &fakeMigrationDB{
		queryResults: []fakeMigrationQueryResult{
			{checksum: schemaChecksum(currentSchemaSQLForTest())},
		},
	}

	if err := applyCurrentSchema(context.Background(), db); err != nil {
		t.Fatalf("applyCurrentSchema() unexpected error: %v", err)
	}

	if len(db.execCalls) != 1 {
		t.Fatalf("expected only schema_migrations bootstrap exec, got %d calls", len(db.execCalls))
	}
}

func TestApplyCurrentSchema_FailsOnChecksumMismatch(t *testing.T) {
	t.Parallel()

	db := &fakeMigrationDB{
		queryResults: []fakeMigrationQueryResult{
			{checksum: "different"},
		},
	}

	err := applyCurrentSchema(context.Background(), db)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected checksum mismatch error, got %v", err)
	}
}

func TestApplyCurrentSchema_FailsWhenUserTablesExistWithoutRecordedVersion(t *testing.T) {
	t.Parallel()

	db := &fakeMigrationDB{
		queryResults: []fakeMigrationQueryResult{
			{err: pgx.ErrNoRows},
			{count: 3},
		},
	}

	err := applyCurrentSchema(context.Background(), db)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "database already has 3 user tables") {
		t.Fatalf("expected existing tables error, got %v", err)
	}
}

func TestApplyCurrentSchema_WrapsMigrationLoadError(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("query failed")
	db := &fakeMigrationDB{
		queryResults: []fakeMigrationQueryResult{
			{err: expectedErr},
		},
	}

	err := applyCurrentSchema(context.Background(), db)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected wrapped error %v, got %v", expectedErr, err)
	}
}

func TestSplitSQLStatements_IgnoresSemicolonsInCommentsAndQuotedBodies(t *testing.T) {
	t.Parallel()

	script := `
CREATE TABLE test_a (id INT);
-- semicolon in comment ;
CREATE FUNCTION test_fn() RETURNS void AS $fn$
BEGIN
    PERFORM 1;
END;
$fn$ LANGUAGE plpgsql;
INSERT INTO test_a VALUES ('value;still-string');
`

	statements := splitSQLStatements(script)
	expected := []string{
		"CREATE TABLE test_a (id INT)",
		"-- semicolon in comment ;\nCREATE FUNCTION test_fn() RETURNS void AS $fn$\nBEGIN\n    PERFORM 1;\nEND;\n$fn$ LANGUAGE plpgsql",
		"INSERT INTO test_a VALUES ('value;still-string')",
	}
	if !reflect.DeepEqual(statements, expected) {
		t.Fatalf("unexpected statements: %#v", statements)
	}
}

type fakeMigrationDB struct {
	execCalls    []fakeMigrationExecCall
	queryResults []fakeMigrationQueryResult
	execErr      map[string]error
	tx           *fakeMigrationTx
}

type fakeMigrationExecCall struct {
	sql       string
	arguments []any
}

type fakeMigrationQueryResult struct {
	checksum string
	count    int64
	err      error
}

func (f *fakeMigrationDB) Exec(_ context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	f.execCalls = append(f.execCalls, fakeMigrationExecCall{
		sql:       sql,
		arguments: append([]any(nil), arguments...),
	})
	if f.execErr != nil {
		if err, ok := f.execErr[strings.TrimSpace(sql)]; ok {
			return pgconn.CommandTag{}, err
		}
	}
	return pgconn.CommandTag{}, nil
}

func (f *fakeMigrationDB) Begin(context.Context) (pgx.Tx, error) {
	if f.tx == nil {
		f.tx = &fakeMigrationTx{}
	}
	return f.tx, nil
}

func (f *fakeMigrationDB) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	if len(f.queryResults) == 0 {
		return fakeMigrationRow{err: errors.New("unexpected query")}
	}
	result := f.queryResults[0]
	f.queryResults = f.queryResults[1:]
	return fakeMigrationRow{checksum: result.checksum, count: result.count, err: result.err}
}

type fakeMigrationTx struct {
	execCalls  []fakeMigrationExecCall
	committed  bool
	rolledBack bool
	execErr    map[string]error
}

func (f *fakeMigrationTx) Begin(context.Context) (pgx.Tx, error) {
	return nil, errors.New("unexpected nested begin")
}
func (f *fakeMigrationTx) Commit(context.Context) error {
	f.committed = true
	return nil
}
func (f *fakeMigrationTx) Rollback(context.Context) error {
	f.rolledBack = true
	return nil
}
func (f *fakeMigrationTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, errors.New("unexpected copy from")
}
func (f *fakeMigrationTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults { return nil }
func (f *fakeMigrationTx) LargeObjects() pgx.LargeObjects                         { return pgx.LargeObjects{} }
func (f *fakeMigrationTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, errors.New("unexpected prepare")
}
func (f *fakeMigrationTx) Exec(_ context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	f.execCalls = append(f.execCalls, fakeMigrationExecCall{
		sql:       sql,
		arguments: append([]any(nil), arguments...),
	})
	if f.execErr != nil {
		if err, ok := f.execErr[strings.TrimSpace(sql)]; ok {
			return pgconn.CommandTag{}, err
		}
	}
	return pgconn.CommandTag{}, nil
}
func (f *fakeMigrationTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected query")
}
func (f *fakeMigrationTx) QueryRow(context.Context, string, ...any) pgx.Row {
	return fakeMigrationRow{err: errors.New("unexpected query row")}
}
func (f *fakeMigrationTx) Conn() *pgx.Conn { return nil }

type fakeMigrationRow struct {
	checksum string
	count    int64
	err      error
}

func (r fakeMigrationRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != 1 {
		return errors.New("expected exactly one scan destination")
	}
	switch target := dest[0].(type) {
	case *string:
		*target = r.checksum
	case *int64:
		*target = r.count
	default:
		return errors.New("expected *string or *int64 destination")
	}
	return nil
}

func currentSchemaSQLForTest() string {
	return shivaschema.InitialSchemaSQL()
}
