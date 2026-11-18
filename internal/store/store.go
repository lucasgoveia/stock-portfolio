// Package store opens SQLite and applies the versioned Atlas migrations.
//
// Schema changes are authored as desired state in schema/ and turned into
// timestamped migrations with `atlas migrate diff <name> --env local` (see
// atlas.hcl). The migration directory is embedded and applied on Open; its
// atlas.sum checksum is verified first, so an edited applied migration fails
// loudly instead of drifting.
package store

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"ariga.io/atlas/sql/migrate"
	"ariga.io/atlas/sql/sqlite"
	_ "modernc.org/sqlite" // pure-Go SQLite driver
)

//go:embed migrations
var migrations embed.FS

// Open opens the SQLite database at path with WAL and foreign keys enabled
// and applies all pending migrations.
func Open(path string) (*sql.DB, error) {
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if err := Migrate(context.Background(), conn); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

// Dir returns the embedded migration directory.
func Dir() (migrate.Dir, error) {
	dir := &migrate.MemDir{}
	entries, err := fs.ReadDir(migrations, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}
	for _, e := range entries {
		b, err := fs.ReadFile(migrations, "migrations/"+e.Name())
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", e.Name(), err)
		}
		if err := dir.WriteFile(e.Name(), b); err != nil {
			return nil, fmt.Errorf("load migration %s: %w", e.Name(), err)
		}
	}
	return dir, nil
}

// Migrate applies pending migrations. Each file runs in its own transaction.
func Migrate(ctx context.Context, conn *sql.DB) error {
	// One connection: SQLite pragmas and transactions must see the same session.
	c, err := conn.Conn(ctx)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	defer func() { _ = c.Close() }()

	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := migrate.Validate(dir); err != nil {
		return fmt.Errorf("migrate: checksum (run `atlas migrate hash` after editing migrations): %w", err)
	}
	drv, err := sqlite.Open(c)
	if err != nil {
		return fmt.Errorf("migrate: driver: %w", err)
	}
	rrw, err := newRevisions(ctx, c)
	if err != nil {
		return err
	}
	ex, err := migrate.NewExecutor(drv, dir, rrw)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	if err := ex.ExecuteN(ctx, 0); err != nil && !errors.Is(err, migrate.ErrNoPendingFiles) {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}

// revisions stores applied migrations as JSON in schema_revisions.
type revisions struct{ c *sql.Conn }

// storedRevision mirrors migrate.Revision for storage: the upstream JSON tags
// drop Hash and PartialHashes and RevisionType has no text unmarshaler.
type storedRevision struct {
	Version         string
	Description     string
	Type            uint
	Applied         int
	Total           int
	ExecutedAt      time.Time
	ExecutionTime   time.Duration
	Error           string
	ErrorStmt       string
	Hash            string
	PartialHashes   []string
	OperatorVersion string
}

func encodeRevision(r *migrate.Revision) ([]byte, error) {
	return json.Marshal(storedRevision{
		Version: r.Version, Description: r.Description, Type: uint(r.Type), Applied: r.Applied, Total: r.Total,
		ExecutedAt: r.ExecutedAt, ExecutionTime: r.ExecutionTime, Error: r.Error, ErrorStmt: r.ErrorStmt,
		Hash: r.Hash, PartialHashes: r.PartialHashes, OperatorVersion: r.OperatorVersion,
	})
}

func decodeRevision(s string) (*migrate.Revision, error) {
	var r storedRevision
	if err := json.Unmarshal([]byte(s), &r); err != nil {
		return nil, fmt.Errorf("decode revision: %w", err)
	}
	return &migrate.Revision{
		Version: r.Version, Description: r.Description, Type: migrate.RevisionType(r.Type), Applied: r.Applied,
		Total: r.Total, ExecutedAt: r.ExecutedAt, ExecutionTime: r.ExecutionTime, Error: r.Error,
		ErrorStmt: r.ErrorStmt, Hash: r.Hash, PartialHashes: r.PartialHashes, OperatorVersion: r.OperatorVersion,
	}, nil
}

func newRevisions(ctx context.Context, c *sql.Conn) (*revisions, error) {
	if _, err := c.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_revisions (
		version  TEXT PRIMARY KEY,
		revision TEXT NOT NULL
	)`); err != nil {
		return nil, fmt.Errorf("migrate: revisions table: %w", err)
	}
	return &revisions{c: c}, nil
}

func (r *revisions) Ident() *migrate.TableIdent {
	return &migrate.TableIdent{Name: "schema_revisions"}
}

func (r *revisions) ReadRevisions(ctx context.Context) ([]*migrate.Revision, error) {
	rows, err := r.c.QueryContext(ctx, `SELECT revision FROM schema_revisions ORDER BY version`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []*migrate.Revision
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		rev, err := decodeRevision(s)
		if err != nil {
			return nil, err
		}
		out = append(out, rev)
	}
	return out, rows.Err()
}

func (r *revisions) ReadRevision(ctx context.Context, v string) (*migrate.Revision, error) {
	var s string
	err := r.c.QueryRowContext(ctx, `SELECT revision FROM schema_revisions WHERE version = ?`, v).Scan(&s)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, migrate.ErrRevisionNotExist
	}
	if err != nil {
		return nil, err
	}
	return decodeRevision(s)
}

func (r *revisions) WriteRevision(ctx context.Context, rev *migrate.Revision) error {
	b, err := encodeRevision(rev)
	if err != nil {
		return err
	}
	_, err = r.c.ExecContext(ctx, `INSERT INTO schema_revisions (version, revision) VALUES (?, ?)
		ON CONFLICT (version) DO UPDATE SET revision = excluded.revision`, rev.Version, string(b))
	return err
}

func (r *revisions) DeleteRevision(ctx context.Context, v string) error {
	_, err := r.c.ExecContext(ctx, `DELETE FROM schema_revisions WHERE version = ?`, v)
	return err
}
