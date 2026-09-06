package db

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const maxStoredSlugMisses = 500
const maxListedSlugMisses = 100

type SlugMiss struct {
	Slug          string `json:"slug"`
	MissCount     int64  `json:"miss_count"`
	FirstMissedAt string `json:"first_missed_at"`
	LastMissedAt  string `json:"last_missed_at"`
}

func (db *DbWrapper) RecordSlugMiss(ctx context.Context, slug string, missedAt time.Time) error {
	slug = strings.ToLower(slug)
	value := missedAt.UTC().Format(time.RFC3339Nano)
	tx, err := db.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin record slug miss: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO slug_misses (slug, miss_count, first_missed_at, last_missed_at)
		SELECT ?, 1, ?, ?
		WHERE NOT EXISTS (SELECT 1 FROM redirects WHERE slug = ?)
		ON CONFLICT(slug) DO UPDATE SET
			miss_count = CASE WHEN miss_count < 9223372036854775807 THEN miss_count + 1 ELSE miss_count END,
			first_missed_at = CASE WHEN julianday(first_missed_at) < julianday(excluded.first_missed_at) THEN first_missed_at ELSE excluded.first_missed_at END,
			last_missed_at = CASE WHEN julianday(last_missed_at) > julianday(excluded.last_missed_at) THEN last_missed_at ELSE excluded.last_missed_at END
	`, slug, value, value, slug); err != nil {
		return fmt.Errorf("record slug miss: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM slug_misses
		WHERE slug IN (
			SELECT slug FROM slug_misses
			ORDER BY julianday(last_missed_at) DESC, slug COLLATE NOCASE
			LIMIT -1 OFFSET ?
		)
	`, maxStoredSlugMisses); err != nil {
		return fmt.Errorf("prune slug misses: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit record slug miss: %w", err)
	}
	return nil
}

func (db *DbWrapper) ListSlugMisses() ([]SlugMiss, error) {
	rows, err := db.Query(`
		SELECT slug, miss_count, first_missed_at, last_missed_at
		FROM slug_misses
		ORDER BY julianday(last_missed_at) DESC, slug COLLATE NOCASE
		LIMIT ?
	`, maxListedSlugMisses)
	if err != nil {
		return nil, fmt.Errorf("list slug misses: %w", err)
	}
	defer rows.Close()

	misses := make([]SlugMiss, 0)
	for rows.Next() {
		var miss SlugMiss
		if err := rows.Scan(&miss.Slug, &miss.MissCount, &miss.FirstMissedAt, &miss.LastMissedAt); err != nil {
			return nil, fmt.Errorf("scan slug miss: %w", err)
		}
		misses = append(misses, miss)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list slug misses: %w", err)
	}
	return misses, nil
}
