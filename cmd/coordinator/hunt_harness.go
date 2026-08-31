package main

import (
	"context"
	"database/sql"

	"hackme/internal/hunt"
)

func huntPutHarnessArtifact(ctx context.Context, db *sql.DB, hash string, data []byte, sourceRel string) error {
	return hunt.PutHarnessArtifact(ctx, db, hash, data, sourceRel)
}

func huntGetHarnessArtifact(ctx context.Context, db *sql.DB, hash string) ([]byte, error) {
	return hunt.GetHarnessArtifact(ctx, db, hash)
}
