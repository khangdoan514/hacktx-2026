package definitions

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
)

const (
	// ActorHeader names the person or service recorded as creator, tester,
	// publisher, or retirer. The website sends it on every mutation.
	ActorHeader = "X-NMC3-Actor"

	pathDefinitions = "/v1/definitions"
	pathDraft       = "/v1/definitions/draft"
	pathTest        = "/v1/definitions/test"
	pathPublish     = "/v1/definitions/publish"
	pathRevise      = "/v1/definitions/revise"
	pathRetire      = "/v1/definitions/retire"
	pathExport      = "/v1/definitions/export"
)

// Handler serves the definition lifecycle at /v1/definitions.
// nmc3-agent mounts it when DATABASE_URL is set. Run start still loads
// examples/definitions.json and does not call ResolveAndPinRun.
func Handler(reg *Registry) http.Handler {
	mux := http.NewServeMux()
	Mount(mux, reg)
	return mux
}

// Mount registers the definition routes on mux. Patterns are absolute
// (/v1/definitions/...). Register these before a catch-all so they win
// over the run API:
//
//	mux := http.NewServeMux()
//	definitions.Mount(mux, reg)
//	mux.Handle("/", httpapi.New(coordinator, runs, events, fileDefinitions))
func Mount(mux *http.ServeMux, reg *Registry) {
	h := &httpAPI{reg: reg}
	mux.HandleFunc("GET "+pathDefinitions, h.getOrList)
	mux.HandleFunc("GET "+pathExport, h.export)
	mux.HandleFunc("POST "+pathDraft, h.draft)
	mux.HandleFunc("POST "+pathTest, h.test)
	mux.HandleFunc("POST "+pathPublish, h.publish)
	mux.HandleFunc("POST "+pathRevise, h.revise)
	mux.HandleFunc("POST "+pathRetire, h.retire)
}

type httpAPI struct {
	reg *Registry
}

type definitionResponse struct {
	Definition Version `json:"definition"`
}

type definitionListResponse struct {
	Definitions []Version `json:"definitions"`
}

type errorResponse struct {
	Error errorObject `json:"error"`
}

type errorObject struct {
	Code    string       `json:"code"`
	Message string       `json:"message"`
	Fields  []FieldError `json:"fields,omitempty"`
}

type versionRef struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

type reviseRequest struct {
	ID          string          `json:"id"`
	FromVersion string          `json:"from_version"`
	Spec        json.RawMessage `json:"spec"`
}

func (h *httpAPI) draft(w http.ResponseWriter, r *http.Request) {
	actor, body, ok := h.mutation(w, r)
	if !ok {
		return
	}
	version, err := h.reg.SaveDraftJSON(r.Context(), actor, body)
	if err != nil {
		writeRegistryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, definitionResponse{Definition: version})
}

func (h *httpAPI) publish(w http.ResponseWriter, r *http.Request) {
	actor, body, ok := h.mutation(w, r)
	if !ok {
		return
	}
	version, err := h.reg.PublishJSON(r.Context(), actor, body)
	if err != nil {
		writeRegistryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, definitionResponse{Definition: version})
}

func (h *httpAPI) test(w http.ResponseWriter, r *http.Request) {
	actor, raw, ok := h.mutation(w, r)
	if !ok {
		return
	}
	var ref versionRef
	if err := decodeObject(raw, &ref); err != nil {
		writeRegistryError(w, err)
		return
	}
	version, err := h.reg.Test(r.Context(), actor, ref.ID, ref.Version)
	if err != nil {
		writeRegistryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, definitionResponse{Definition: version})
}

func (h *httpAPI) revise(w http.ResponseWriter, r *http.Request) {
	actor, raw, ok := h.mutation(w, r)
	if !ok {
		return
	}
	var req reviseRequest
	if err := decodeObject(raw, &req); err != nil {
		writeRegistryError(w, err)
		return
	}
	fields := make([]FieldError, 0)
	if req.ID == "" {
		fields = append(fields, FieldError{Field: "id", Message: "is required"})
	}
	if req.FromVersion == "" {
		fields = append(fields, FieldError{Field: "from_version", Message: "is required"})
	}
	if len(bytes.TrimSpace(req.Spec)) == 0 || string(bytes.TrimSpace(req.Spec)) == "null" {
		fields = append(fields, FieldError{Field: "spec", Message: "is required"})
	}
	if len(fields) > 0 {
		writeRegistryError(w, validationError(fields))
		return
	}
	spec, specFields := decodeSpec(req.Spec)
	if len(specFields) > 0 {
		writeRegistryError(w, validationError(specFields))
		return
	}
	if spec.ID != req.ID {
		writeRegistryError(w, &Error{Code: CodeInvalidInput, Message: "revised definition id does not match the source", Fields: []FieldError{{Field: "id", Message: "must match spec.id"}}})
		return
	}
	version, err := h.reg.Revise(r.Context(), actor, req.ID, req.FromVersion, spec)
	if err != nil {
		writeRegistryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, definitionResponse{Definition: version})
}

func (h *httpAPI) retire(w http.ResponseWriter, r *http.Request) {
	actor, raw, ok := h.mutation(w, r)
	if !ok {
		return
	}
	var ref versionRef
	if err := decodeObject(raw, &ref); err != nil {
		writeRegistryError(w, err)
		return
	}
	version, err := h.reg.Retire(r.Context(), actor, ref.ID, ref.Version)
	if err != nil {
		writeRegistryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, definitionResponse{Definition: version})
}

func (h *httpAPI) getOrList(w http.ResponseWriter, r *http.Request) {
	id, idOK := singleQuery(r, "id")
	version, versionOK := singleQuery(r, "version")
	if !idOK || !versionOK {
		writeRegistryError(w, &Error{Code: CodeInvalidInput, Message: "invalid query"})
		return
	}
	if id == "" && version == "" {
		versions, err := h.reg.List(r.Context())
		if err != nil {
			writeRegistryError(w, err)
			return
		}
		if versions == nil {
			versions = []Version{}
		}
		writeJSON(w, http.StatusOK, definitionListResponse{Definitions: versions})
		return
	}
	if id == "" || version == "" {
		fields := make([]FieldError, 0, 2)
		if id == "" {
			fields = append(fields, FieldError{Field: "id", Message: "is required"})
		}
		if version == "" {
			fields = append(fields, FieldError{Field: "version", Message: "is required"})
		}
		writeRegistryError(w, validationError(fields))
		return
	}
	found, err := h.reg.Get(r.Context(), id, version)
	if err != nil {
		writeRegistryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, definitionResponse{Definition: found})
}

func (h *httpAPI) export(w http.ResponseWriter, r *http.Request) {
	body, err := h.reg.Export(r.Context())
	if err != nil {
		writeRegistryError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (h *httpAPI) mutation(w http.ResponseWriter, r *http.Request) (string, []byte, bool) {
	actor := strings.TrimSpace(r.Header.Get(ActorHeader))
	if err := checkActor(actor); err != nil {
		writeRegistryError(w, err)
		return "", nil, false
	}
	body, err := readBody(w, r)
	if err != nil {
		writeRegistryError(w, err)
		return "", nil, false
	}
	return actor, body, true
}

func readBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var max *http.MaxBytesError
		if errors.As(err, &max) {
			return nil, &Error{Code: CodeInvalidInput, Message: "request body is too large"}
		}
		return nil, &Error{Code: CodeInvalidInput, Message: "invalid request body"}
	}
	return body, nil
}

func decodeObject(raw []byte, dest any) error {
	dec := json.NewDecoder(bytes.NewReader(bytes.TrimSpace(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil {
		if name, ok := unknownFieldName(err); ok {
			return &Error{Code: CodeInvalidInput, Message: "unknown field", Fields: []FieldError{{Field: name, Message: "unknown field"}}}
		}
		return &Error{Code: CodeInvalidInput, Message: "invalid request JSON"}
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return &Error{Code: CodeInvalidInput, Message: "request must contain one JSON object"}
	}
	return nil
}

func singleQuery(r *http.Request, name string) (string, bool) {
	vals, ok := r.URL.Query()[name]
	if !ok {
		return "", true
	}
	if len(vals) != 1 {
		return "", false
	}
	return vals[0], true
}

func writeRegistryError(w http.ResponseWriter, err error) {
	var reg *Error
	if errors.As(err, &reg) {
		writeJSON(w, statusOf(reg.Code), errorResponse{Error: errorObject{Code: reg.Code, Message: reg.Message, Fields: reg.Fields}})
		return
	}
	writeJSON(w, http.StatusInternalServerError, errorResponse{Error: errorObject{Code: string(contracts.ErrInternal), Message: "internal server error"}})
}

func statusOf(code string) int {
	switch code {
	case CodeInvalidInput:
		return http.StatusBadRequest
	case CodeNotFound:
		return http.StatusNotFound
	case CodeRetired, CodeImmutable, CodeConflict:
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		http.Error(w, `{"error":{"code":"INTERNAL","message":"internal server error"}}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}
