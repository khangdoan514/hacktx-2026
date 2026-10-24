package capabilities

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"

	"github.com/jackc/pgx/v5"
	"github.com/khangdoan514/hacktx-2026/internal/audit"
	"github.com/khangdoan514/hacktx-2026/internal/organization"
	"github.com/khangdoan514/hacktx-2026/internal/platform"
)

var materialKeys = []string{"tools", "context", "model_policy", "instructions", "output_action_types", "budget", "resources", "evaluations"}

type Spec struct {
	ID               string          `json:"id"`
	RequestID        string          `json:"request_id"`
	State            string          `json:"state"`
	Version          int             `json:"version"`
	CurrentRevision  int             `json:"current_revision"`
	MaterialRevision int             `json:"material_revision"`
	Document         json.RawMessage `json:"document"`
}

func material(previous, next json.RawMessage) bool {
	var left, right map[string]any
	_ = json.Unmarshal(previous, &left)
	_ = json.Unmarshal(next, &right)
	if left == nil {
		return true
	}
	for _, key := range materialKeys {
		if !reflect.DeepEqual(left[key], right[key]) {
			return true
		}
	}
	return false
}

func CreateSpec(ctx context.Context, tx pgx.Tx, actor platform.Actor, requestID string, document json.RawMessage) (Spec, error) {
	if err := organization.Require(actor, "ai_engineer", "administrator"); err != nil {
		return Spec{}, err
	}
	if !json.Valid(document) {
		return Spec{}, platform.Fail(http.StatusUnprocessableEntity, "validation", "The specification document must be JSON")
	}
	var state string
	if err := tx.QueryRow(ctx, `SELECT state FROM app.capability_requests WHERE id = $1`, requestID).Scan(&state); err != nil {
		return Spec{}, platform.Fail(http.StatusNotFound, "not_found", "Record not found")
	}
	if state != "accepted" && state != "in_delivery" {
		return Spec{}, platform.Fail(http.StatusConflict, "invalid_state", "Accept the request before writing a specification")
	}
	spec := Spec{ID: platform.UUID(), RequestID: requestID, State: "draft", Version: 1, CurrentRevision: 1, MaterialRevision: 1, Document: document}
	_, err := tx.Exec(ctx, `
		INSERT INTO app.specifications (id, organization_id, request_id, state, current_revision, material_revision, created_by)
		VALUES ($1,$2,$3,'draft',1,1,$4)
	`, spec.ID, actor.OrgID, requestID, actor.ID)
	if err != nil {
		return Spec{}, err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO app.specification_revisions (specification_id, revision, document, material, created_by)
		VALUES ($1,1,$2::jsonb,true,$3)
	`, spec.ID, string(document), actor.ID)
	if err != nil {
		return Spec{}, err
	}
	return spec, audit.Write(ctx, tx, actor, "specification.drafted", "specification", spec.ID, spec)
}

func Revise(ctx context.Context, tx pgx.Tx, actor platform.Actor, specID string, version int, document json.RawMessage) (Spec, error) {
	if err := organization.Require(actor, "ai_engineer", "administrator"); err != nil {
		return Spec{}, err
	}
	current, err := GetSpec(ctx, tx, specID)
	if err != nil {
		return Spec{}, err
	}
	if current.Version != version {
		return Spec{}, platform.Fail(http.StatusConflict, "conflict", "The specification changed. Reload and try again.")
	}
	changed := material(current.Document, document)
	nextRevision := current.CurrentRevision + 1
	materialRevision := current.MaterialRevision
	state := current.State
	if changed {
		materialRevision = nextRevision
		if _, err := tx.Exec(ctx, `SELECT app.note_material_revision($1)`, specID); err != nil {
			return Spec{}, err
		}
		state = "draft"
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO app.specification_revisions (specification_id, revision, document, material, created_by)
		VALUES ($1,$2,$3::jsonb,$4,$5)
	`, specID, nextRevision, string(document), changed, actor.ID)
	if err != nil {
		return Spec{}, err
	}
	_, err = tx.Exec(ctx, `
		UPDATE app.specifications
		SET current_revision = $2, material_revision = $3, state = $4, version = version + 1, updated_at = now()
		WHERE id = $1 AND version = $5
	`, specID, nextRevision, materialRevision, state, version)
	if err != nil {
		return Spec{}, err
	}
	return GetSpec(ctx, tx, specID)
}

func SubmitReview(ctx context.Context, tx pgx.Tx, actor platform.Actor, specID string, version int) (Spec, error) {
	if err := organization.Require(actor, "ai_engineer", "administrator"); err != nil {
		return Spec{}, err
	}
	tag, err := tx.Exec(ctx, `
		UPDATE app.specifications
		SET state = 'in_review', version = version + 1, updated_at = now()
		WHERE id = $1 AND organization_id = $2 AND version = $3 AND state IN ('draft','changes_requested')
	`, specID, actor.OrgID, version)
	if err != nil {
		return Spec{}, err
	}
	if tag.RowsAffected() != 1 {
		return Spec{}, platform.Fail(http.StatusConflict, "conflict", "The specification is not ready for review")
	}
	return GetSpec(ctx, tx, specID)
}

func GetSpec(ctx context.Context, tx pgx.Tx, id string) (Spec, error) {
	var spec Spec
	err := tx.QueryRow(ctx, `
		SELECT spec.id::text, spec.request_id::text, spec.state, spec.version, spec.current_revision, spec.material_revision, revision.document
		FROM app.specifications AS spec
		JOIN app.specification_revisions AS revision
		  ON revision.specification_id = spec.id AND revision.revision = spec.current_revision
		WHERE spec.id = $1
	`, id).Scan(&spec.ID, &spec.RequestID, &spec.State, &spec.Version, &spec.CurrentRevision, &spec.MaterialRevision, &spec.Document)
	if platform.Missing(err) {
		return Spec{}, platform.Fail(http.StatusNotFound, "not_found", "Record not found")
	}
	return spec, err
}

func SpecForRequest(ctx context.Context, tx pgx.Tx, requestID string) (Spec, error) {
	var id string
	err := tx.QueryRow(ctx, `SELECT id::text FROM app.specifications WHERE request_id = $1`, requestID).Scan(&id)
	if platform.Missing(err) {
		return Spec{}, nil
	}
	if err != nil {
		return Spec{}, err
	}
	return GetSpec(ctx, tx, id)
}
