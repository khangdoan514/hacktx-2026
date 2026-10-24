package capabilities

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/khangdoan514/hacktx-2026/internal/audit"
	"github.com/khangdoan514/hacktx-2026/internal/organization"
	"github.com/khangdoan514/hacktx-2026/internal/platform"
)

type Release struct {
	ID                string `json:"id"`
	SpecificationID   string `json:"specification_id"`
	SpecRevision      int    `json:"spec_revision"`
	State             string `json:"state"`
	Version           int    `json:"version"`
	HardwareAllocated bool   `json:"hardware_allocated"`
	GrantsWriteTool   bool   `json:"grants_write_tool"`
}

func OpenCatalog(ctx context.Context, tx pgx.Tx, actor platform.Actor, specID string) (Release, error) {
	if err := organization.Require(actor, "ai_engineer", "release_approver", "administrator"); err != nil {
		return Release{}, err
	}
	spec, err := GetSpec(ctx, tx, specID)
	if err != nil {
		return Release{}, err
	}
	if spec.State != "approved" {
		return Release{}, platform.Fail(http.StatusConflict, "invalid_state", "Approve the specification before opening a catalog release")
	}
	item := Release{ID: platform.UUID(), SpecificationID: specID, SpecRevision: spec.MaterialRevision, State: "draft", Version: 1}
	_, err = tx.Exec(ctx, `
		INSERT INTO app.catalog_releases (id, organization_id, specification_id, spec_revision, state, created_by)
		VALUES ($1,$2,$3,$4,'draft',$5)
	`, item.ID, actor.OrgID, specID, spec.MaterialRevision, actor.ID)
	return item, err
}

func StartTesting(ctx context.Context, tx pgx.Tx, actor platform.Actor, id string, version int) (Release, error) {
	return moveCatalog(ctx, tx, actor, id, version, "draft", "testing")
}

func RecordEvaluation(ctx context.Context, tx pgx.Tx, actor platform.Actor, catalogID, evidence string, passed bool) (Release, error) {
	if err := organization.Require(actor, "ai_engineer", "release_approver", "administrator"); err != nil {
		return Release{}, err
	}
	item, err := GetCatalog(ctx, tx, catalogID)
	if err != nil {
		return Release{}, err
	}
	if item.State != "testing" && item.State != "eligible" {
		return Release{}, platform.Fail(http.StatusConflict, "invalid_state", "Start testing before recording an evaluation")
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO app.evaluation_records (id, organization_id, specification_id, catalog_id, spec_revision, gate, passed, evidence, created_by)
		VALUES ($1,$2,$3,$4,$5,'release',$6,$7,$8)
	`, platform.UUID(), actor.OrgID, item.SpecificationID, catalogID, item.SpecRevision, passed, evidence, actor.ID)
	if err != nil {
		return Release{}, err
	}
	if passed {
		_, err = tx.Exec(ctx, `
			UPDATE app.catalog_releases SET state = 'eligible', version = version + 1, updated_at = now()
			WHERE id = $1 AND state = 'testing'
		`, catalogID)
	}
	if err != nil {
		return Release{}, err
	}
	return GetCatalog(ctx, tx, catalogID)
}

func Publish(ctx context.Context, tx pgx.Tx, actor platform.Actor, id string, version int) (Release, error) {
	if err := organization.Require(actor, "release_approver", "administrator"); err != nil {
		return Release{}, err
	}
	item, err := GetCatalog(ctx, tx, id)
	if err != nil {
		return Release{}, err
	}
	if item.State != "eligible" || item.Version != version {
		return Release{}, platform.Fail(http.StatusConflict, "invalid_state", "A passing evaluation is required before publication")
	}
	var passed bool
	err = tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM app.evaluation_records
			WHERE catalog_id = $1 AND spec_revision = $2 AND gate = 'release' AND passed AND invalidated_at IS NULL
		)
	`, id, item.SpecRevision).Scan(&passed)
	if err != nil {
		return Release{}, err
	}
	if !passed {
		return Release{}, platform.Fail(http.StatusConflict, "invalid_state", "Evaluation evidence is missing or no longer valid")
	}
	tag, err := tx.Exec(ctx, `
		UPDATE app.catalog_releases
		SET state = 'published', published_at = now(), version = version + 1, updated_at = now()
		WHERE id = $1 AND state = 'eligible' AND version = $2
	`, id, version)
	if err != nil {
		return Release{}, err
	}
	if tag.RowsAffected() != 1 {
		return Release{}, platform.Fail(http.StatusConflict, "conflict", "The catalog release changed")
	}
	published, err := GetCatalog(ctx, tx, id)
	if err != nil {
		return Release{}, err
	}
	published.HardwareAllocated = false
	published.GrantsWriteTool = false
	return published, audit.Write(ctx, tx, actor, "catalog.published", "catalog", id, published)
}

func Suspend(ctx context.Context, tx pgx.Tx, actor platform.Actor, id string, version int) (Release, error) {
	return moveCatalog(ctx, tx, actor, id, version, "published", "suspended")
}

func Retire(ctx context.Context, tx pgx.Tx, actor platform.Actor, id string, version int) (Release, error) {
	item, err := GetCatalog(ctx, tx, id)
	if err != nil {
		return Release{}, err
	}
	if item.State != "published" && item.State != "suspended" {
		return Release{}, platform.Fail(http.StatusConflict, "invalid_state", "Only a published or suspended release can be retired")
	}
	return moveCatalog(ctx, tx, actor, id, version, item.State, "retired")
}

func moveCatalog(ctx context.Context, tx pgx.Tx, actor platform.Actor, id string, version int, from, to string) (Release, error) {
	if err := organization.Require(actor, "ai_engineer", "release_approver", "administrator"); err != nil {
		return Release{}, err
	}
	tag, err := tx.Exec(ctx, `
		UPDATE app.catalog_releases
		SET state = $4, version = version + 1, updated_at = now()
		WHERE id = $1 AND organization_id = $2 AND state = $3 AND version = $5
	`, id, actor.OrgID, from, to, version)
	if err != nil {
		return Release{}, err
	}
	if tag.RowsAffected() != 1 {
		return Release{}, platform.Fail(http.StatusConflict, "conflict", "That catalog transition is not available")
	}
	return GetCatalog(ctx, tx, id)
}

func GetCatalog(ctx context.Context, tx pgx.Tx, id string) (Release, error) {
	var item Release
	err := tx.QueryRow(ctx, `
		SELECT id::text, specification_id::text, spec_revision, state, version
		FROM app.catalog_releases WHERE id = $1
	`, id).Scan(&item.ID, &item.SpecificationID, &item.SpecRevision, &item.State, &item.Version)
	if platform.Missing(err) {
		return Release{}, platform.Fail(http.StatusNotFound, "not_found", "Record not found")
	}
	return item, err
}
