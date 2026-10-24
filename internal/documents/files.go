package documents

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"

	"github.com/jackc/pgx/v5"
	"github.com/khangdoan514/hacktx-2026/internal/platform"
)

type File struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
	State    string `json:"state"`
	Checksum string `json:"checksum,omitempty"`
	Size     int64  `json:"byte_size,omitempty"`
}

func Initiate(ctx context.Context, tx pgx.Tx, actor platform.Actor, requestID, filename string) (File, error) {
	item := File{ID: platform.UUID(), Filename: filename, State: "initiated"}
	_, err := tx.Exec(ctx, `
		INSERT INTO app.private_files (id, organization_id, request_id, owner_id, filename, state)
		VALUES ($1,$2,$3,$4,$5,'initiated')
	`, item.ID, actor.OrgID, requestID, actor.ID, filename)
	return item, err
}

func WriteBytes(root string, id string, data []byte) error {
	if err := os.MkdirAll(root, 0o750); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, id), data, 0o640)
}

func Finalize(ctx context.Context, tx pgx.Tx, actor platform.Actor, root, id, checksum string) (File, error) {
	data, err := os.ReadFile(filepath.Join(root, id))
	if err != nil {
		return File{}, platform.Fail(http.StatusConflict, "conflict", "Upload the bytes before finalizing")
	}
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	if got != checksum {
		return File{}, platform.Fail(http.StatusConflict, "conflict", "The checksum does not match the stored bytes")
	}
	tag, err := tx.Exec(ctx, `
		UPDATE app.private_files
		SET state = 'ready', checksum = $3, byte_size = $4, storage_key = $1
		WHERE id = $1 AND owner_id = $2 AND state = 'initiated'
	`, id, actor.ID, checksum, len(data))
	if err != nil {
		return File{}, err
	}
	if tag.RowsAffected() != 1 {
		return File{}, platform.Fail(http.StatusConflict, "conflict", "The file is not awaiting finalization")
	}
	return File{ID: id, State: "ready", Checksum: checksum, Size: int64(len(data))}, nil
}

func Open(ctx context.Context, tx pgx.Tx, root, id string) (File, []byte, error) {
	var item File
	err := tx.QueryRow(ctx, `
		SELECT id::text, filename, state, COALESCE(checksum, ''), COALESCE(byte_size, 0)
		FROM app.private_files WHERE id = $1 AND state = 'ready'
	`, id).Scan(&item.ID, &item.Filename, &item.State, &item.Checksum, &item.Size)
	if platform.Missing(err) {
		return File{}, nil, platform.Fail(http.StatusNotFound, "not_found", "Record not found")
	}
	if err != nil {
		return File{}, nil, err
	}
	data, err := os.ReadFile(filepath.Join(root, id))
	return item, data, err
}
