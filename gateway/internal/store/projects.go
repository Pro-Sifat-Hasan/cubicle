package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

const projectsSchema = `
CREATE TABLE IF NOT EXISTS projects (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL COLLATE NOCASE UNIQUE,
	path TEXT NOT NULL DEFAULT '',
	notes TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);
`

// MaxProjects caps the projects the staff keep notes on.
const MaxProjects = 50

var ErrTooManyProjects = errors.New("too many projects")

// Project is something the owner works on: where it lives on disk and what the staff
// should know about it ("AbroadMate: a SaaS, Next.js + Postgres, UI in src/components").
type Project struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	Notes     string    `json:"notes"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// UpsertProject creates a project, or updates the one with the same name (any letter case).
// An empty path or notes keeps what is stored, so a note can be added without repeating the path.
func (s *Store) UpsertProject(ctx context.Context, name, path, notes string) (*Project, error) {
	name, path, notes = strings.TrimSpace(name), strings.TrimSpace(path), strings.TrimSpace(notes)
	now := s.now().UTC().UnixMilli()
	res, err := s.db.ExecContext(ctx, `UPDATE projects SET
			path = CASE WHEN ? = '' THEN path ELSE ? END,
			notes = CASE WHEN ? = '' THEN notes ELSE ? END,
			updated_at = ?
		WHERE name = ?`, path, path, notes, notes, now, name)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var count int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM projects`).Scan(&count); err != nil {
			return nil, err
		}
		if count >= MaxProjects {
			return nil, ErrTooManyProjects
		}
		if _, err := s.db.ExecContext(ctx, `INSERT INTO projects (id, name, path, notes, created_at, updated_at)
			VALUES (?,?,?,?,?,?)`, newID("prj"), name, path, notes, now, now); err != nil {
			return nil, err
		}
	}
	return s.projectByName(ctx, name)
}

func (s *Store) projectByName(ctx context.Context, name string) (*Project, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, name, path, notes, created_at, updated_at FROM projects WHERE name = ?`, name)
	p, err := scanProject(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return p, err
}

// ListProjects returns the projects, most recently updated first.
func (s *Store) ListProjects(ctx context.Context) ([]*Project, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, path, notes, created_at, updated_at FROM projects ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Project{}
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) DeleteProject(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM projects WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

type rowScanner interface{ Scan(dest ...any) error }

func scanProject(r rowScanner) (*Project, error) {
	var p Project
	var created, updated int64
	if err := r.Scan(&p.ID, &p.Name, &p.Path, &p.Notes, &created, &updated); err != nil {
		return nil, err
	}
	p.CreatedAt, p.UpdatedAt = fromMS(created), fromMS(updated)
	return &p, nil
}
