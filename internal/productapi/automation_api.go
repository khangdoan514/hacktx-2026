package productapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/khangdoan514/hacktx-2026/internal/organization"
	"github.com/khangdoan514/hacktx-2026/internal/platform"
)

type agentAutomation struct {
	ID           string             `json:"id"`
	ProjectID    *string            `json:"project_id,omitempty"`
	Name         string             `json:"name"`
	AgentID      string             `json:"agent_id"`
	AgentVersion string             `json:"agent_version"`
	Input        json.RawMessage    `json:"input"`
	Limits       json.RawMessage    `json:"limits"`
	Schedule     automationSchedule `json:"schedule"`
	Status       string             `json:"status"`
	NextRunAt    *time.Time         `json:"next_run_at,omitempty"`
	CreatedAt    time.Time          `json:"created_at"`
}

type automationOccurrence struct {
	ID           string    `json:"id"`
	ScheduledFor time.Time `json:"scheduled_for"`
	State        string    `json:"state"`
	AgentRunID   *string   `json:"agent_run_id,omitempty"`
	Attempts     int       `json:"attempts"`
	Error        *string   `json:"error,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type automationLimits struct {
	MaxDelegations      int     `json:"max_delegations"`
	MaxConcurrentAgents int     `json:"max_concurrent_agents"`
	MaxToolCalls        int     `json:"max_tool_calls"`
	MaxModelTurns       int     `json:"max_model_turns"`
	MaxTokens           int     `json:"max_tokens"`
	MaxCostUSD          float64 `json:"max_cost_usd"`
	TimeoutSeconds      int     `json:"timeout_seconds"`
}

func (l automationLimits) valid() bool {
	return l.MaxDelegations > 0 && l.MaxConcurrentAgents > 0 && l.MaxToolCalls > 0 && l.MaxModelTurns > 0 && l.MaxTokens > 0 && l.MaxCostUSD > 0 && l.MaxCostUSD <= 100 && l.TimeoutSeconds > 0 && l.TimeoutSeconds <= 3600
}

func automationPermission(actor platform.Actor) error {
	return organization.Require(actor, "ai_engineer", "operator", "administrator")
}

func (s *Server) createAutomation(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	if err := automationPermission(actor); err != nil {
		return 0, nil, err
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var input struct {
		Name         string             `json:"name"`
		ProjectID    string             `json:"project_id"`
		AgentID      string             `json:"agent_id"`
		AgentVersion string             `json:"agent_version"`
		RunInput     json.RawMessage    `json:"input"`
		Limits       json.RawMessage    `json:"limits"`
		Schedule     automationSchedule `json:"schedule"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
		return 0, nil, platform.Fail(http.StatusBadRequest, "invalid_input", "Invalid automation request")
	}
	input.Name = strings.TrimSpace(input.Name)
	input.AgentID = strings.TrimSpace(input.AgentID)
	input.AgentVersion = strings.TrimSpace(input.AgentVersion)
	if len(input.Name) == 0 || len(input.Name) > 120 || len(input.AgentID) == 0 || len(input.AgentID) > 256 || len(input.AgentVersion) == 0 || len(input.AgentVersion) > 80 || len(input.RunInput) == 0 || !json.Valid(input.RunInput) {
		return 0, nil, platform.Fail(http.StatusBadRequest, "invalid_input", "Name, agent, version, and JSON input are required")
	}
	var limits automationLimits
	if json.Unmarshal(input.Limits, &limits) != nil || !limits.valid() {
		return 0, nil, platform.Fail(http.StatusBadRequest, "invalid_input", "Valid positive run limits are required")
	}
	if input.ProjectID != "" {
		if _, err := projectAccess(ctx, tx, input.ProjectID); err != nil {
			return 0, nil, err
		}
	}
	now := time.Now().UTC()
	if input.Schedule.Timezone == "" {
		input.Schedule.Timezone = "UTC"
	}
	if input.Schedule.Kind == "interval" && input.Schedule.RunAt == nil {
		start := now.Add(time.Duration(input.Schedule.IntervalSeconds) * time.Second)
		input.Schedule.RunAt = &start
	}
	next, err := validateAutomationSchedule(input.Schedule, now)
	if err != nil {
		return 0, nil, platform.Fail(http.StatusBadRequest, "invalid_input", err.Error())
	}
	var project any
	if input.ProjectID != "" {
		project = input.ProjectID
	}
	var id string
	err = tx.QueryRow(ctx, `INSERT INTO app.agent_automations
		(organization_id, owner_id, project_id, name, agent_id, agent_version, input, limits,
		 schedule_kind, run_at, interval_seconds, local_time, weekday, timezone, next_run_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8::jsonb,$9,$10,NULLIF($11,0),NULLIF($12,''),$13,$14,$15)
		RETURNING id::text`, actor.OrgID, actor.ID, project, input.Name, input.AgentID, input.AgentVersion,
		string(input.RunInput), string(input.Limits), input.Schedule.Kind, input.Schedule.RunAt,
		input.Schedule.IntervalSeconds, input.Schedule.LocalTime, input.Schedule.Weekday,
		input.Schedule.Timezone, next).Scan(&id)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusCreated, map[string]any{"id": id, "next_run_at": next}, nil
}

const automationColumns = `id::text, project_id::text, name, agent_id, agent_version, input, limits,
 schedule_kind, run_at, interval_seconds, local_time, weekday, timezone, status, next_run_at, created_at`

func scanAutomation(row interface{ Scan(...any) error }) (agentAutomation, error) {
	var item agentAutomation
	var runAt *time.Time
	var interval *int32
	var localTime *string
	var weekday *int16
	err := row.Scan(&item.ID, &item.ProjectID, &item.Name, &item.AgentID, &item.AgentVersion,
		&item.Input, &item.Limits, &item.Schedule.Kind, &runAt, &interval, &localTime,
		&weekday, &item.Schedule.Timezone, &item.Status, &item.NextRunAt, &item.CreatedAt)
	if err != nil {
		return item, err
	}
	item.Schedule.RunAt = runAt
	if interval != nil {
		item.Schedule.IntervalSeconds = int(*interval)
	}
	if localTime != nil {
		item.Schedule.LocalTime = *localTime
	}
	if weekday != nil {
		day := int(*weekday)
		item.Schedule.Weekday = &day
	}
	return item, nil
}

func (s *Server) listAutomations(_ http.ResponseWriter, _ *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	rows, err := tx.Query(ctx, `SELECT `+automationColumns+` FROM app.agent_automations WHERE owner_id=$1 ORDER BY created_at DESC LIMIT 100`, actor.ID)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()
	items := []agentAutomation{}
	for rows.Next() {
		item, err := scanAutomation(rows)
		if err != nil {
			return 0, nil, err
		}
		items = append(items, item)
	}
	return http.StatusOK, map[string]any{"items": items}, rows.Err()
}

func (s *Server) listAutomationRuns(_ http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	id := r.PathValue("id")
	if !projectUUID.MatchString(id) {
		return 0, nil, platform.Fail(http.StatusBadRequest, "invalid_input", "Invalid automation ID")
	}
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM app.agent_automations WHERE id=$1 AND owner_id=$2)`, id, actor.ID).Scan(&exists); err != nil {
		return 0, nil, err
	}
	if !exists {
		return 0, nil, platform.Fail(http.StatusNotFound, "not_found", "Automation not found")
	}
	rows, err := tx.Query(ctx, `SELECT id::text, scheduled_for, state, agent_run_id, attempts, error, created_at
		FROM app.agent_automation_runs WHERE automation_id=$1 ORDER BY scheduled_for DESC LIMIT 50`, id)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()
	items := []automationOccurrence{}
	for rows.Next() {
		var item automationOccurrence
		if err := rows.Scan(&item.ID, &item.ScheduledFor, &item.State, &item.AgentRunID, &item.Attempts, &item.Error, &item.CreatedAt); err != nil {
			return 0, nil, err
		}
		items = append(items, item)
	}
	return http.StatusOK, map[string]any{"items": items}, rows.Err()
}

func (s *Server) setAutomationStatus(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	if err := automationPermission(actor); err != nil {
		return 0, nil, err
	}
	id := r.PathValue("id")
	if !projectUUID.MatchString(id) {
		return 0, nil, platform.Fail(http.StatusBadRequest, "invalid_input", "Invalid automation ID")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	var input struct {
		Status string `json:"status"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF || (input.Status != "active" && input.Status != "paused") {
		return 0, nil, platform.Fail(http.StatusBadRequest, "invalid_input", "Status must be active or paused")
	}
	item, err := scanAutomation(tx.QueryRow(ctx, `SELECT `+automationColumns+` FROM app.agent_automations WHERE id=$1 AND owner_id=$2 FOR UPDATE`, id, actor.ID))
	if err == pgx.ErrNoRows {
		return 0, nil, platform.Fail(http.StatusNotFound, "not_found", "Automation not found")
	}
	if err != nil {
		return 0, nil, err
	}
	if item.Status == "completed" {
		return 0, nil, platform.Fail(http.StatusConflict, "conflict", "A completed one-time automation cannot be resumed")
	}
	var next *time.Time
	if input.Status == "active" {
		next, err = nextAutomationTime(item.Schedule, time.Now().UTC())
		if err != nil {
			return 0, nil, err
		}
		if next == nil {
			return 0, nil, platform.Fail(http.StatusConflict, "conflict", "This one-time schedule has passed")
		}
	}
	_, err = tx.Exec(ctx, `UPDATE app.agent_automations SET status=$2,next_run_at=$3,updated_at=now() WHERE id=$1`, id, input.Status, next)
	if err != nil {
		return 0, nil, err
	}
	if input.Status == "paused" {
		_, err = tx.Exec(ctx, `UPDATE app.agent_automation_runs SET state='cancelled',updated_at=now()
			WHERE automation_id=$1 AND state='queued'`, id)
		if err != nil {
			return 0, nil, err
		}
	}
	return http.StatusOK, map[string]any{"status": input.Status, "next_run_at": next}, nil
}

func (s *Server) runAutomationNow(_ http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	if err := automationPermission(actor); err != nil {
		return 0, nil, err
	}
	id := r.PathValue("id")
	if !projectUUID.MatchString(id) {
		return 0, nil, platform.Fail(http.StatusBadRequest, "invalid_input", "Invalid automation ID")
	}
	var projectID *string
	var status string
	err := tx.QueryRow(ctx, `SELECT project_id::text,status FROM app.agent_automations WHERE id=$1 AND owner_id=$2`, id, actor.ID).Scan(&projectID, &status)
	if err == pgx.ErrNoRows {
		return 0, nil, platform.Fail(http.StatusNotFound, "not_found", "Automation not found")
	}
	if err != nil {
		return 0, nil, err
	}
	if status != "active" {
		return 0, nil, platform.Fail(http.StatusConflict, "conflict", "Enable the automation before running it")
	}
	if projectID != nil {
		if _, err := projectAccess(ctx, tx, *projectID); err != nil {
			return 0, nil, err
		}
	}
	var runID string
	err = tx.QueryRow(ctx, `INSERT INTO app.agent_automation_runs (automation_id,scheduled_for)
		VALUES ($1,now()) RETURNING id::text`, id).Scan(&runID)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusAccepted, map[string]string{"id": runID, "state": "queued"}, nil
}
