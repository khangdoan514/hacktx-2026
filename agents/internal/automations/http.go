package automations

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
	"github.com/khangdoan514/hacktx-2026/internal/platform"
)

// Check validates a stored run request before it is saved, so a schedule
// does not fire a request the coordinator will refuse every time.
type Check func(ctx context.Context, req contracts.StartRunRequest) error

// API serves:
//
//	GET    /v1/automations                 list, each with its latest firing
//	POST   /v1/automations                 create
//	POST   /v1/automations/preview         next times for a schedule
//	GET    /v1/automations/{id}            one automation
//	PATCH  /v1/automations/{id}            change name, schedule, request, enabled
//	DELETE /v1/automations/{id}            remove it and its firings
//	POST   /v1/automations/{id}/run        fire now
//	GET    /v1/automations/{id}/firings    firing history
//	GET    /v1/automation-firings          recent firings across the tenant
//
// Automations belong to the tenant: everyone in the organization sees them.
// cmd/api decides who may change them.
type API struct {
	Service *Service
	Check   Check
}

func (a *API) Mount(mux *http.ServeMux) {
	mux.HandleFunc("/v1/automations", a.collection)
	mux.HandleFunc("/v1/automations/", a.item)
	mux.HandleFunc("/v1/automation-firings", a.recent)
}

type body struct {
	Name     *string                    `json:"name"`
	Schedule *string                    `json:"schedule"`
	Timezone *string                    `json:"timezone"`
	Enabled  *bool                      `json:"enabled"`
	Request  *contracts.StartRunRequest `json:"request"`
}

func tenant(r *http.Request) string {
	if t := strings.TrimSpace(r.Header.Get(platform.TenantHeader)); t != "" {
		return t
	}
	return "local"
}

func principal(r *http.Request) string {
	if p := strings.TrimSpace(r.Header.Get(platform.UserHeader)); p != "" {
		return p
	}
	return "local"
}

func (a *API) collection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.list(w, r)
	case http.MethodPost:
		a.create(w, r)
	default:
		fail(w, http.StatusMethodNotAllowed, contracts.ErrInvalidInput, "method not allowed")
	}
}

func (a *API) item(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/v1/automations/")
	if rest == "preview" && r.Method == http.MethodPost {
		a.preview(w, r)
		return
	}
	id, action, _ := strings.Cut(rest, "/")
	if id == "" || strings.Contains(action, "/") {
		fail(w, http.StatusNotFound, contracts.ErrNotFound, "endpoint not found")
		return
	}
	switch {
	case action == "" && r.Method == http.MethodGet:
		item, err := a.Service.Store.Get(r.Context(), tenant(r), id)
		if err != nil {
			storeError(w, err)
			return
		}
		a.attachLatest(r.Context(), tenant(r), []*Automation{&item})
		reply(w, http.StatusOK, map[string]any{"automation": item})
	case action == "" && r.Method == http.MethodPatch:
		a.update(w, r, id)
	case action == "" && r.Method == http.MethodDelete:
		if err := a.Service.Store.Delete(r.Context(), tenant(r), id); err != nil {
			storeError(w, err)
			return
		}
		reply(w, http.StatusOK, map[string]string{"status": "deleted"})
	case action == "run" && r.Method == http.MethodPost:
		item, err := a.Service.Store.Get(r.Context(), tenant(r), id)
		if err != nil {
			storeError(w, err)
			return
		}
		firing := a.Service.Fire(r.Context(), item, TriggerManual, a.Service.now())
		status := http.StatusAccepted
		if firing.RunID == "" {
			status = http.StatusUnprocessableEntity
		}
		reply(w, status, map[string]any{"firing": firing})
	case action == "firings" && r.Method == http.MethodGet:
		if _, err := a.Service.Store.Get(r.Context(), tenant(r), id); err != nil {
			storeError(w, err)
			return
		}
		firings, err := a.Service.Firings(r.Context(), tenant(r), id, 50)
		if err != nil {
			storeError(w, err)
			return
		}
		reply(w, http.StatusOK, map[string]any{"firings": firings})
	case action == "" || action == "run" || action == "firings":
		fail(w, http.StatusMethodNotAllowed, contracts.ErrInvalidInput, "method not allowed")
	default:
		fail(w, http.StatusNotFound, contracts.ErrNotFound, "endpoint not found")
	}
}

func (a *API) recent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		fail(w, http.StatusMethodNotAllowed, contracts.ErrInvalidInput, "method not allowed")
		return
	}
	firings, err := a.Service.Firings(r.Context(), tenant(r), "", 60)
	if err != nil {
		storeError(w, err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"firings": firings})
}

func (a *API) list(w http.ResponseWriter, r *http.Request) {
	items, err := a.Service.Store.List(r.Context(), tenant(r))
	if err != nil {
		storeError(w, err)
		return
	}
	ptrs := make([]*Automation, len(items))
	for i := range items {
		ptrs[i] = &items[i]
	}
	a.attachLatest(r.Context(), tenant(r), ptrs)
	reply(w, http.StatusOK, map[string]any{"automations": items})
}

// attachLatest fills LastFiring from one query over recent tenant firings.
func (a *API) attachLatest(ctx context.Context, tenant string, items []*Automation) {
	if len(items) == 0 {
		return
	}
	automationID := ""
	if len(items) == 1 {
		automationID = items[0].ID
	}
	firings, err := a.Service.Firings(ctx, tenant, automationID, 200)
	if err != nil {
		return
	}
	latest := map[string]*Firing{}
	for i := range firings {
		if _, ok := latest[firings[i].AutomationID]; !ok {
			latest[firings[i].AutomationID] = &firings[i]
		}
	}
	for _, item := range items {
		item.LastFiring = latest[item.ID]
	}
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		fail(w, http.StatusBadRequest, contracts.ErrInvalidInput, "invalid request JSON")
		return false
	}
	return true
}

func (a *API) preview(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Schedule string `json:"schedule"`
		Timezone string `json:"timezone"`
	}
	if !decode(w, r, &in) {
		return
	}
	sched, err := ParseSchedule(in.Schedule, in.Timezone)
	if err != nil {
		fail(w, http.StatusBadRequest, contracts.ErrInvalidInput, err.Error())
		return
	}
	next := []time.Time{}
	t := a.Service.now()
	for len(next) < 5 {
		t = sched.Next(t)
		if t.IsZero() {
			break
		}
		next = append(next, t)
	}
	reply(w, http.StatusOK, map[string]any{"next": next})
}

// apply validates the merged automation and recomputes its next slot.
func (a *API) apply(ctx context.Context, item *Automation, in body) (string, bool) {
	if in.Name != nil {
		item.Name = strings.TrimSpace(*in.Name)
	}
	if in.Schedule != nil {
		item.Schedule = strings.TrimSpace(*in.Schedule)
	}
	if in.Timezone != nil {
		item.Timezone = strings.TrimSpace(*in.Timezone)
	}
	if item.Timezone == "" {
		item.Timezone = "UTC"
	}
	if in.Enabled != nil {
		item.Enabled = *in.Enabled
	}
	if in.Request != nil {
		item.Request = *in.Request
	}
	if item.Name == "" || len(item.Name) > 120 {
		return "name is required (up to 120 characters)", false
	}
	sched, err := ParseSchedule(item.Schedule, item.Timezone)
	if err != nil {
		return err.Error(), false
	}
	if item.Request.AgentID == "" || item.Request.AgentVersion == "" || len(item.Request.Input) == 0 || !json.Valid(item.Request.Input) {
		return "request needs agent_id, agent_version, and JSON input", false
	}
	if a.Check != nil {
		if err := a.Check(ctx, item.Request); err != nil {
			var runErr *contracts.RunError
			if errors.As(err, &runErr) {
				return runErr.Message, false
			}
			return "the run request is not valid", false
		}
	}
	item.NextRunAt = nil
	if item.Enabled {
		if next := sched.Next(a.Service.now()); !next.IsZero() {
			item.NextRunAt = &next
		}
	}
	item.UpdatedAt = a.Service.now()
	return "", true
}

func (a *API) create(w http.ResponseWriter, r *http.Request) {
	var in body
	if !decode(w, r, &in) {
		return
	}
	now := a.Service.now()
	item := Automation{
		ID: newID("auto-"), TenantID: tenant(r), CreatedBy: principal(r),
		CreatedByEmail: r.Header.Get(platform.EmailHeader), Enabled: true, CreatedAt: now,
	}
	if msg, ok := a.apply(r.Context(), &item, in); !ok {
		fail(w, http.StatusBadRequest, contracts.ErrInvalidInput, msg)
		return
	}
	if err := a.Service.Store.Create(r.Context(), item); err != nil {
		storeError(w, err)
		return
	}
	reply(w, http.StatusCreated, map[string]any{"automation": item})
}

func (a *API) update(w http.ResponseWriter, r *http.Request, id string) {
	var in body
	if !decode(w, r, &in) {
		return
	}
	item, err := a.Service.Store.Get(r.Context(), tenant(r), id)
	if err != nil {
		storeError(w, err)
		return
	}
	if msg, ok := a.apply(r.Context(), &item, in); !ok {
		fail(w, http.StatusBadRequest, contracts.ErrInvalidInput, msg)
		return
	}
	if err := a.Service.Store.Update(r.Context(), item); err != nil {
		storeError(w, err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"automation": item})
}

func storeError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrNotFound) {
		fail(w, http.StatusNotFound, contracts.ErrNotFound, "automation not found")
		return
	}
	fail(w, http.StatusInternalServerError, contracts.ErrInternal, "automations could not be read or saved")
}

func fail(w http.ResponseWriter, status int, code contracts.ErrorCode, message string) {
	reply(w, status, contracts.ErrorResponse{Error: contracts.RunError{Code: code, Message: message}})
}

func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
