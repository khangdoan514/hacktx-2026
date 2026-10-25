package threads

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore persists the product thread index and transcript snapshots.
type PostgresStore struct{ Pool *pgxpool.Pool }

func (s PostgresStore) Create(ctx context.Context, t Thread) error {
	if t.TenantID == "" {
		t.TenantID = "local"
	}
	if t.PrincipalID == "" {
		t.PrincipalID = "local"
	}
	_, err := s.Pool.Exec(ctx, `insert into nmc3.threads(id,tenant_id,principal_id,title,transcript,created_at,updated_at) values($1,$2,$3,$4,$5,$6,$6)`, t.ID, t.TenantID, t.PrincipalID, t.Title, []byte("[]"), time.Now().UTC())
	return err
}

func (s PostgresStore) Save(ctx context.Context, t Thread) error {
	if t.TenantID == "" {
		t.TenantID = "local"
	}
	if t.PrincipalID == "" {
		t.PrincipalID = "local"
	}
	raw, err := json.Marshal(t.Messages)
	if err != nil {
		return err
	}
	_, err = s.Pool.Exec(ctx, `update nmc3.threads set title=$4,transcript=$5,busy=$6,updated_at=$7 where id=$1 and tenant_id=$2 and principal_id=$3`, t.ID, t.TenantID, t.PrincipalID, t.Title, raw, t.Busy, time.Now().UTC())
	return err
}

func (s PostgresStore) Get(ctx context.Context, id, tenant, principal string) (Thread, error) {
	if tenant == "" {
		tenant = "local"
	}
	if principal == "" {
		principal = "local"
	}
	var t Thread
	var raw []byte
	err := s.Pool.QueryRow(ctx, `select id,title,transcript,updated_at from nmc3.threads where id=$1 and tenant_id=$2 and principal_id=$3 and archived_at is null`, id, tenant, principal).Scan(&t.ID, &t.Title, &raw, &t.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Thread{}, err
	}
	if err != nil {
		return Thread{}, err
	}
	t.TenantID = tenant
	t.PrincipalID = principal
	if err = json.Unmarshal(raw, &t.Messages); err != nil {
		return Thread{}, err
	}
	if t.Messages == nil {
		t.Messages = []Message{}
	}
	return t, nil
}

func (s PostgresStore) List(ctx context.Context, tenant, principal string) ([]Thread, error) {
	if tenant == "" {
		tenant = "local"
	}
	if principal == "" {
		principal = "local"
	}
	rows, err := s.Pool.Query(ctx, `select id,title,updated_at,busy from nmc3.threads where tenant_id=$1 and principal_id=$2 and archived_at is null order by updated_at desc limit 100`, tenant, principal)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Thread{}
	for rows.Next() {
		var t Thread
		if err := rows.Scan(&t.ID, &t.Title, &t.UpdatedAt, &t.Busy); err != nil {
			return nil, err
		}
		items = append(items, t)
	}
	return items, rows.Err()
}

func (s PostgresStore) Archive(ctx context.Context, id, tenant, principal string) error {
	if tenant == "" {
		tenant = "local"
	}
	if principal == "" {
		principal = "local"
	}
	tag, err := s.Pool.Exec(ctx, `update nmc3.threads set archived_at=now(),updated_at=now() where id=$1 and tenant_id=$2 and principal_id=$3`, id, tenant, principal)
	if err == nil && tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return err
}

func (s PostgresStore) AddFile(ctx context.Context, a Attachment) error {
	if a.TenantID == "" {
		a.TenantID = "local"
	}
	if a.PrincipalID == "" {
		a.PrincipalID = "local"
	}
	_, err := s.Pool.Exec(ctx, `insert into nmc3.thread_files(id,thread_id,tenant_id,principal_id,object_key,file_name,content_type,byte_size,checksum_sha256) select $1,$2,$3,$4,$5,$6,$7,$8,$9 where exists(select 1 from nmc3.threads where id=$2 and tenant_id=$3 and principal_id=$4)`, a.ID, a.ThreadID, a.TenantID, a.PrincipalID, a.ObjectKey, a.Name, a.ContentType, a.Size, a.Checksum)
	return err
}

func (s PostgresStore) Files(ctx context.Context, threadID, tenant, principal string) ([]Attachment, error) {
	if tenant == "" {
		tenant = "local"
	}
	if principal == "" {
		principal = "local"
	}
	rows, err := s.Pool.Query(ctx, `select id,thread_id,object_key,file_name,content_type,byte_size,checksum_sha256,created_at from nmc3.thread_files where thread_id=$1 and tenant_id=$2 and principal_id=$3 order by created_at`, threadID, tenant, principal)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Attachment{}
	for rows.Next() {
		var a Attachment
		if err := rows.Scan(&a.ID, &a.ThreadID, &a.ObjectKey, &a.Name, &a.ContentType, &a.Size, &a.Checksum, &a.CreatedAt); err != nil {
			return nil, err
		}
		a.TenantID = tenant
		a.PrincipalID = principal
		items = append(items, a)
	}
	return items, rows.Err()
}

func (s PostgresStore) File(ctx context.Context, id, tenant, principal string) (Attachment, error) {
	if tenant == "" {
		tenant = "local"
	}
	if principal == "" {
		principal = "local"
	}
	var a Attachment
	err := s.Pool.QueryRow(ctx, `select id,thread_id,object_key,file_name,content_type,byte_size,checksum_sha256,created_at from nmc3.thread_files where id=$1 and tenant_id=$2 and principal_id=$3`, id, tenant, principal).Scan(&a.ID, &a.ThreadID, &a.ObjectKey, &a.Name, &a.ContentType, &a.Size, &a.Checksum, &a.CreatedAt)
	a.TenantID = tenant
	a.PrincipalID = principal
	return a, err
}
