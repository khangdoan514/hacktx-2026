package definitions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var specKeys = map[string]bool{
	"id": true, "version": true, "name": true, "role": true, "instructions": true,
	"input_schema": true, "output_schema": true, "allowed_tools": true, "allowed_delegates": true,
	"limits": true, "model_policy": true,
}

var limitKeys = map[string]bool{
	"max_delegations": true, "max_concurrent_agents": true, "max_tool_calls": true,
	"max_model_turns": true, "max_tokens": true, "max_cost_usd": true, "timeout_seconds": true,
}

var policyKeys = map[string]bool{
	"default_model": true, "allowed_models": true,
}

// Parse reads a JSON array of definitions or an envelope
// {"schema_version":"1","definitions":[...]}. Backup documents with a
// versions field are imported through Import, not Parse.
func Parse(raw []byte) ([]Spec, error) {
	specs, fields, err := parseDefinitionDocument(raw)
	if err != nil {
		return nil, err
	}
	if len(fields) > 0 {
		return nil, validationError(fields)
	}
	return specs, nil
}

// MarshalDefinitions encodes specs as a canonical schema-1 envelope.
// Specs are ordered by id then version. Lifecycle metadata is not included.
func MarshalDefinitions(specs []Spec) ([]byte, error) {
	ordered := append([]Spec(nil), specs...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].ID == ordered[j].ID {
			return ordered[i].Version < ordered[j].Version
		}
		return ordered[i].ID < ordered[j].ID
	})
	defs := make([]any, 0, len(ordered))
	for _, spec := range ordered {
		raw, _, err := Canonical(spec)
		if err != nil {
			return nil, err
		}
		value, err := decodeCanonical(raw)
		if err != nil {
			return nil, err
		}
		defs = append(defs, value)
	}
	return canonicalJSON(map[string]any{
		"schema_version": SchemaVersion,
		"definitions":    defs,
	})
}

// Import reads a definition array, a definitions envelope, or an Export backup.
// Arrays and definition envelopes use mode. Backups restore recorded lifecycle
// and ignore mode. The write is all or nothing.
func (r *Registry) Import(ctx context.Context, actor string, raw []byte, mode ImportMode) (ImportResult, error) {
	if err := checkActor(actor); err != nil {
		return ImportResult{}, err
	}
	classifiedDoc, err := classifyImport(raw)
	if err != nil {
		return ImportResult{}, err
	}
	switch classifiedDoc.kind {
	case importSpecsKind:
		if mode != ImportDraft && mode != ImportPublish {
			return ImportResult{}, &Error{Code: CodeInvalidInput, Message: "import mode must be draft or publish"}
		}
		specs, fields := classifiedDoc.body.specs, classifiedDoc.body.fields
		if len(fields) > 0 {
			return ImportResult{}, validationError(fields)
		}
		return r.importSpecs(ctx, actor, specs, mode)
	case importBackupKind:
		if len(classifiedDoc.body.fields) > 0 {
			return ImportResult{}, validationError(classifiedDoc.body.fields)
		}
		return r.importBackup(ctx, classifiedDoc.body.tools, classifiedDoc.body.versions)
	default:
		return ImportResult{}, &Error{Code: CodeInvalidInput, Message: "unsupported import document"}
	}
}

// Export writes a canonical backup of tools and definition versions.
// Import of that backup into an empty registry reproduces the same hashes.
func (r *Registry) Export(ctx context.Context) ([]byte, error) {
	var tools []Tool
	var versions []Version
	err := r.store.Apply(ctx, func(s txStore) error {
		var err error
		tools, err = s.ListTools(ctx)
		if err != nil {
			return err
		}
		versions, err = s.ListVersions(ctx)
		return err
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(tools, func(i, j int) bool {
		if tools[i].ID == tools[j].ID {
			return tools[i].Version < tools[j].Version
		}
		return tools[i].ID < tools[j].ID
	})
	sortVersions(versions)
	return canonicalJSON(backupDocument{
		SchemaVersion: SchemaVersion,
		Tools:         tools,
		Versions:      versions,
	})
}

type importKind int

const (
	importSpecsKind importKind = iota
	importBackupKind
)

type importPayload struct {
	specs    []Spec
	versions []Version
	tools    []Tool
	fields   []FieldError
}

type backupDocument struct {
	SchemaVersion string    `json:"schema_version"`
	Tools         []Tool    `json:"tools"`
	Versions      []Version `json:"versions"`
}

type definitionsDocument struct {
	SchemaVersion string            `json:"schema_version"`
	Definitions   []json.RawMessage `json:"definitions"`
	Versions      []json.RawMessage `json:"versions"`
	Tools         []json.RawMessage `json:"tools"`
}

func parseDefinitionDocument(raw []byte) ([]Spec, []FieldError, error) {
	payload, err := classifyImport(raw)
	if err != nil {
		return nil, nil, err
	}
	kind, body := payload.kind, payload.body
	if kind == importBackupKind {
		return nil, nil, &Error{Code: CodeInvalidInput, Message: "backup documents are imported with Import"}
	}
	if len(body.fields) > 0 {
		return nil, body.fields, nil
	}
	return body.specs, nil, nil
}

type classified struct {
	kind importKind
	body importPayload
}

func classifyImport(raw []byte) (classified, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return classified{}, &Error{Code: CodeInvalidInput, Message: "import document is empty"}
	}
	if trimmed[0] == '[' {
		specs, fields := decodeSpecArray(trimmed)
		return classified{kind: importSpecsKind, body: importPayload{specs: specs, fields: fields}}, nil
	}
	if trimmed[0] != '{' {
		return classified{}, &Error{Code: CodeInvalidInput, Message: "import document must be a JSON array or object"}
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.DisallowUnknownFields()
	var doc definitionsDocument
	if err := dec.Decode(&doc); err != nil {
		if name, ok := unknownFieldName(err); ok {
			return classified{}, &Error{Code: CodeInvalidInput, Message: "unknown import field", Fields: []FieldError{{Field: name, Message: "unknown field"}}}
		}
		return classified{}, &Error{Code: CodeInvalidInput, Message: "invalid import document"}
	}
	if dec.More() {
		return classified{}, &Error{Code: CodeInvalidInput, Message: "import document has trailing data"}
	}
	if doc.SchemaVersion != SchemaVersion {
		return classified{}, &Error{Code: CodeInvalidInput, Message: "unsupported schema version", Fields: []FieldError{{Field: "schema_version", Message: "unsupported schema version"}}}
	}
	if len(doc.Definitions) > 0 && len(doc.Versions) > 0 {
		return classified{}, &Error{Code: CodeInvalidInput, Message: "import document cannot contain both definitions and versions"}
	}
	if doc.Versions != nil {
		tools, versions, fields := decodeBackup(doc)
		return classified{kind: importBackupKind, body: importPayload{tools: tools, versions: versions, fields: fields}}, nil
	}
	if doc.Definitions == nil {
		return classified{}, &Error{Code: CodeInvalidInput, Message: "import document needs definitions or versions"}
	}
	specs, fields := decodeSpecValues(doc.Definitions)
	return classified{kind: importSpecsKind, body: importPayload{specs: specs, fields: fields}}, nil
}

func decodeBackup(doc definitionsDocument) ([]Tool, []Version, []FieldError) {
	fields := make([]FieldError, 0)
	tools := make([]Tool, 0, len(doc.Tools))
	for i, raw := range doc.Tools {
		tool, toolFields := decodeTool(raw)
		fields = append(fields, prefixFields(fmt.Sprintf("tools[%d]", i), toolFields)...)
		if len(toolFields) == 0 {
			tools = append(tools, tool)
		}
	}
	versions := make([]Version, 0, len(doc.Versions))
	seen := map[string]int{}
	for i, raw := range doc.Versions {
		version, versionFields := decodeExportedVersion(raw)
		prefixed := prefixFields(fmt.Sprintf("versions[%d]", i), versionFields)
		fields = append(fields, prefixed...)
		if len(versionFields) == 0 {
			key := refKey(version.Spec.ID, version.Spec.Version)
			if prev, ok := seen[key]; ok {
				fields = append(fields, FieldError{Field: fmt.Sprintf("versions[%d].spec.id", i), Message: fmt.Sprintf("duplicate of versions[%d]", prev)})
			}
			seen[key] = i
			versions = append(versions, version)
		}
	}
	if doc.Versions == nil {
		fields = append(fields, FieldError{Field: "versions", Message: "is required"})
	}
	return tools, versions, sortFields(fields)
}

func (r *Registry) importSpecs(ctx context.Context, actor string, specs []Spec, mode ImportMode) (ImportResult, error) {
	var result ImportResult
	err := r.store.Apply(ctx, func(s txStore) error {
		refs, err := loadRefs(ctx, s)
		if err != nil {
			return err
		}
		refMode := RefNone
		state := StateDraft
		if mode == ImportPublish {
			refMode = RefPublish
			state = StatePublished
		}
		for _, spec := range specs {
			spec = normalize(spec)
			refs.Agents[refKey(spec.ID, spec.Version)] = state
		}
		var fields []FieldError
		prepared := make([]Spec, len(specs))
		hashes := make([]string, len(specs))
		for i, spec := range specs {
			spec = normalize(spec)
			if refMode != RefNone {
				got := Validate(spec, refs, refMode)
				fields = append(fields, prefixFields(fmt.Sprintf("definitions[%d]", i), got)...)
			} else if got := Validate(spec, nil, RefNone); len(got) > 0 {
				fields = append(fields, prefixFields(fmt.Sprintf("definitions[%d]", i), got)...)
			}
			ready, _, hash, err := prepare(spec)
			if err != nil {
				return err
			}
			prepared[i] = ready
			hashes[i] = hash
		}
		if len(fields) > 0 {
			return validationError(fields)
		}
		result.Versions = make([]Version, 0, len(prepared))
		for i, spec := range prepared {
			var written Version
			var kind string
			var writeErr error
			if mode == ImportPublish {
				kind, written, writeErr = r.writePublished(ctx, s, actor, spec, hashes[i])
			} else {
				kind, written, writeErr = r.writeDraft(ctx, s, actor, spec, hashes[i])
			}
			if writeErr != nil {
				return prefixWrite(i, writeErr)
			}
			countResult(&result, kind)
			result.Versions = append(result.Versions, written)
		}
		return nil
	})
	if err != nil {
		return ImportResult{}, mapMissing(err, "definition version not found")
	}
	for i, v := range result.Versions {
		checked, err := verify(v)
		if err != nil {
			return ImportResult{}, err
		}
		result.Versions[i] = checked
	}
	return result, nil
}

func (r *Registry) writePublished(ctx context.Context, s txStore, actor string, spec Spec, hash string) (string, Version, error) {
	existing, err := s.GetVersion(ctx, spec.ID, spec.Version)
	if err != nil && !errors.Is(err, errNotFound) {
		return "", Version{}, err
	}
	if err == nil {
		if existing.ContentHash == hash && existing.State == StatePublished {
			return "unchanged", existing, nil
		}
		if existing.State == StatePublished || existing.State == StateRetired {
			return "", Version{}, &Error{Code: CodeImmutable, Message: "published version cannot change; save a new version"}
		}
	}
	written, err := r.publish(ctx, s, actor, spec, hash)
	if err != nil {
		return "", Version{}, err
	}
	if existing.State == StateDraft || existing.State == StateTesting {
		return "updated", written, nil
	}
	return "created", written, nil
}

func (r *Registry) writeDraft(ctx context.Context, s txStore, actor string, spec Spec, hash string) (string, Version, error) {
	existing, err := s.GetVersion(ctx, spec.ID, spec.Version)
	if err != nil && !errors.Is(err, errNotFound) {
		return "", Version{}, err
	}
	if err == nil && (existing.State == StatePublished || existing.State == StateRetired) {
		if existing.ContentHash == hash && existing.State == StatePublished {
			return "", Version{}, &Error{Code: CodeImmutable, Message: "published version cannot change; save a new version"}
		}
		return "", Version{}, &Error{Code: CodeImmutable, Message: "published version cannot change; save a new version"}
	}
	if err == nil && existing.ContentHash == hash && existing.State == StateDraft {
		return "unchanged", existing, nil
	}
	kind := "created"
	if err == nil {
		kind = "updated"
	}
	written, err := r.saveDraft(ctx, s, actor, spec, hash)
	if err != nil {
		return "", Version{}, err
	}
	return kind, written, nil
}

func (r *Registry) importBackup(ctx context.Context, tools []Tool, versions []Version) (ImportResult, error) {
	var result ImportResult
	err := r.store.Apply(ctx, func(s txStore) error {
		for i, tool := range tools {
			raw, hash, err := hashTool(tool)
			if err != nil {
				return err
			}
			_ = raw
			if err := s.InsertTool(ctx, tool, hash, r.now()); err != nil {
				return prefixWriteIndex("tools", i, err)
			}
		}
		refs, err := loadRefs(ctx, s)
		if err != nil {
			return err
		}
		for _, version := range versions {
			refs.Agents[refKey(version.Spec.ID, version.Spec.Version)] = version.State
		}
		var fields []FieldError
		for i, version := range versions {
			mode := RefNone
			switch version.State {
			case StatePublished:
				mode = RefPublish
			case StateTesting, StateDraft:
				mode = RefTest
			}
			if mode != RefNone {
				got := Validate(version.Spec, refs, mode)
				fields = append(fields, prefixFields(fmt.Sprintf("versions[%d].spec", i), got)...)
			} else if got := Validate(version.Spec, nil, RefNone); len(got) > 0 {
				fields = append(fields, prefixFields(fmt.Sprintf("versions[%d].spec", i), got)...)
			}
		}
		if len(fields) > 0 {
			return validationError(fields)
		}
		result.Versions = make([]Version, 0, len(versions))
		for i, version := range versions {
			kind, written, err := r.writeRestored(ctx, s, version)
			if err != nil {
				return prefixWriteIndex("versions", i, err)
			}
			countResult(&result, kind)
			result.Versions = append(result.Versions, written)
		}
		return nil
	})
	if err != nil {
		return ImportResult{}, mapMissing(err, "definition version not found")
	}
	for i, version := range result.Versions {
		checked, err := verify(version)
		if err != nil {
			return ImportResult{}, err
		}
		result.Versions[i] = checked
	}
	return result, nil
}

func (r *Registry) writeRestored(ctx context.Context, s txStore, version Version) (string, Version, error) {
	prepared, _, hash, err := prepare(version.Spec)
	if err != nil {
		return "", Version{}, err
	}
	if hash != version.ContentHash {
		return "", Version{}, &Error{Code: CodeInvalidInput, Message: "backup content hash does not match the spec", Fields: []FieldError{{Field: "content_hash", Message: "does not match the spec"}}}
	}
	version.Spec = prepared
	version.TenantID = r.tenant
	version.SchemaVersion = SchemaVersion
	existing, err := s.GetVersion(ctx, version.Spec.ID, version.Spec.Version)
	if err != nil && !errors.Is(err, errNotFound) {
		return "", Version{}, err
	}
	if err == nil {
		if existing.ContentHash == hash && existing.State == version.State {
			return "unchanged", existing, nil
		}
		return "", Version{}, &Error{Code: CodeConflict, Message: "stored version does not match the backup"}
	}
	if err := s.NameAvailable(ctx, version.Spec.Name, version.Spec.ID); err != nil {
		return "", Version{}, err
	}
	if err := s.EnsureAgent(ctx, version); err != nil {
		return "", Version{}, err
	}
	if err := s.PutVersion(ctx, version); err != nil {
		return "", Version{}, err
	}
	if err := s.RefreshAgent(ctx, version.Spec.ID, version.Spec.Name, version.UpdatedAt); err != nil {
		return "", Version{}, err
	}
	written, err := s.GetVersion(ctx, version.Spec.ID, version.Spec.Version)
	if err != nil {
		return "", Version{}, err
	}
	return "created", written, nil
}

func countResult(result *ImportResult, kind string) {
	switch kind {
	case "created":
		result.Created++
	case "updated":
		result.Updated++
	case "unchanged":
		result.Unchanged++
	}
}

func prefixWrite(index int, err error) error {
	return prefixWriteIndex("definitions", index, err)
}

func prefixWriteIndex(name string, index int, err error) error {
	var reg *Error
	if errors.As(err, &reg) && len(reg.Fields) > 0 {
		copied := *reg
		copied.Fields = prefixFields(fmt.Sprintf("%s[%d]", name, index), reg.Fields)
		return &copied
	}
	return err
}

func decodeSpecArray(raw []byte) ([]Spec, []FieldError) {
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, []FieldError{{Field: "$", Message: "must be a JSON array"}}
	}
	return decodeSpecValues(items)
}

func decodeSpecValues(items []json.RawMessage) ([]Spec, []FieldError) {
	specs := make([]Spec, 0, len(items))
	fields := make([]FieldError, 0)
	seen := map[string]int{}
	for i, item := range items {
		spec, itemFields := decodeSpec(item)
		fields = append(fields, prefixFields(fmt.Sprintf("definitions[%d]", i), itemFields)...)
		if len(itemFields) == 0 {
			key := refKey(spec.ID, spec.Version)
			if prev, ok := seen[key]; ok {
				fields = append(fields, FieldError{Field: fmt.Sprintf("definitions[%d].id", i), Message: fmt.Sprintf("duplicate of definitions[%d]", prev)})
			}
			seen[key] = i
			specs = append(specs, spec)
		}
	}
	return specs, sortFields(fields)
}

func decodeSpec(raw []byte) (Spec, []FieldError) {
	if len(bytes.TrimSpace(raw)) == 0 || !json.Valid(raw) {
		return Spec{}, []FieldError{{Field: "$", Message: "must be JSON"}}
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return Spec{}, []FieldError{{Field: "$", Message: "must be a JSON object"}}
	}
	fields := make([]FieldError, 0)
	for key := range obj {
		if !specKeys[key] {
			fields = append(fields, FieldError{Field: key, Message: "unknown field"})
		}
	}
	expectString(obj, "id", &fields)
	expectString(obj, "version", &fields)
	expectString(obj, "name", &fields)
	expectString(obj, "role", &fields)
	expectString(obj, "instructions", &fields)
	expectObject(obj, "input_schema", &fields)
	expectObject(obj, "output_schema", &fields)
	expectStringArray(obj, "allowed_tools", &fields)
	expectStringArray(obj, "allowed_delegates", &fields)
	if rawLimits, ok := obj["limits"]; ok {
		fields = append(fields, decodeLimitTypes(rawLimits)...)
	} else {
		fields = append(fields, FieldError{Field: "limits", Message: "is required"})
	}
	if rawPolicy, ok := obj["model_policy"]; ok {
		fields = append(fields, decodePolicyTypes(rawPolicy)...)
	}
	if len(fields) > 0 {
		return Spec{}, sortFields(fields)
	}
	var spec Spec
	if err := json.Unmarshal(raw, &spec); err != nil {
		return Spec{}, []FieldError{{Field: "$", Message: "invalid definition JSON"}}
	}
	return spec, nil
}

func decodeLimitTypes(raw json.RawMessage) []FieldError {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return []FieldError{{Field: "limits", Message: "must be an object"}}
	}
	fields := make([]FieldError, 0)
	for key, value := range obj {
		if !limitKeys[key] {
			fields = append(fields, FieldError{Field: "limits." + key, Message: "unknown field"})
			continue
		}
		if !isJSONNumber(value) {
			fields = append(fields, FieldError{Field: "limits." + key, Message: "must be a number"})
		}
	}
	return fields
}

func decodePolicyTypes(raw json.RawMessage) []FieldError {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return []FieldError{{Field: "model_policy", Message: "must be an object"}}
	}
	fields := make([]FieldError, 0)
	for key := range obj {
		if !policyKeys[key] {
			fields = append(fields, FieldError{Field: "model_policy." + key, Message: "unknown field"})
		}
	}
	if value, ok := obj["default_model"]; ok {
		var text string
		if json.Unmarshal(value, &text) != nil {
			fields = append(fields, FieldError{Field: "model_policy.default_model", Message: "must be a string"})
		}
	}
	if value, ok := obj["allowed_models"]; ok {
		var models []string
		if json.Unmarshal(value, &models) != nil {
			fields = append(fields, FieldError{Field: "model_policy.allowed_models", Message: "must be an array of strings"})
		}
	}
	return fields
}

func decodeTool(raw []byte) (Tool, []FieldError) {
	if len(bytes.TrimSpace(raw)) == 0 || !json.Valid(raw) {
		return Tool{}, []FieldError{{Field: "$", Message: "must be JSON"}}
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var tool Tool
	if err := dec.Decode(&tool); err != nil {
		if name, ok := unknownFieldName(err); ok {
			return Tool{}, []FieldError{{Field: name, Message: "unknown field"}}
		}
		return Tool{}, []FieldError{{Field: "$", Message: "invalid tool JSON"}}
	}
	if fields := validateTool(tool); len(fields) > 0 {
		return Tool{}, fields
	}
	return tool, nil
}

func decodeExportedVersion(raw []byte) (Version, []FieldError) {
	if len(bytes.TrimSpace(raw)) == 0 || !json.Valid(raw) {
		return Version{}, []FieldError{{Field: "$", Message: "must be JSON"}}
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var body struct {
		Spec          json.RawMessage `json:"spec"`
		SchemaVersion string          `json:"schema_version"`
		State         string          `json:"state"`
		ContentHash   string          `json:"content_hash"`
		CreatedBy     string          `json:"created_by"`
		CreatedAt     time.Time       `json:"created_at"`
		UpdatedAt     time.Time       `json:"updated_at"`
		TestedBy      string          `json:"tested_by"`
		TestedAt      *time.Time      `json:"tested_at"`
		PublishedBy   string          `json:"published_by"`
		PublishedAt   *time.Time      `json:"published_at"`
		RetiredBy     string          `json:"retired_by"`
		RetiredAt     *time.Time      `json:"retired_at"`
	}
	if err := dec.Decode(&body); err != nil {
		if name, ok := unknownFieldName(err); ok {
			return Version{}, []FieldError{{Field: name, Message: "unknown field"}}
		}
		return Version{}, []FieldError{{Field: "$", Message: "invalid version JSON"}}
	}
	spec, specFields := decodeSpec(body.Spec)
	if len(specFields) > 0 {
		return Version{}, prefixFields("spec", specFields)
	}
	version := Version{
		Spec: spec, SchemaVersion: body.SchemaVersion, State: body.State, ContentHash: body.ContentHash,
		CreatedBy: body.CreatedBy, CreatedAt: body.CreatedAt.UTC(), UpdatedAt: body.UpdatedAt.UTC(),
		TestedBy: body.TestedBy, TestedAt: utcPtr(body.TestedAt),
		PublishedBy: body.PublishedBy, PublishedAt: utcPtr(body.PublishedAt),
		RetiredBy: body.RetiredBy, RetiredAt: utcPtr(body.RetiredAt),
	}
	fields := validateExported(version)
	_, _, hash, err := prepare(spec)
	if err != nil {
		fields = append(fields, FieldError{Field: "spec", Message: "cannot canonicalize spec"})
	} else if body.ContentHash != hash {
		fields = append(fields, FieldError{Field: "content_hash", Message: "does not match the spec"})
	}
	if len(fields) > 0 {
		return Version{}, sortFields(fields)
	}
	version.ContentHash = hash
	return version, nil
}

func validateExported(v Version) []FieldError {
	fields := make([]FieldError, 0)
	add := func(field, message string) {
		fields = append(fields, FieldError{Field: field, Message: message})
	}
	if v.SchemaVersion != SchemaVersion {
		add("schema_version", "unsupported schema version")
	}
	if !validDisplay(v.CreatedBy, maxActorLen) {
		add("created_by", "is required")
	}
	if v.CreatedAt.IsZero() {
		add("created_at", "is required")
	}
	if v.UpdatedAt.IsZero() {
		add("updated_at", "is required")
	}
	switch v.State {
	case StateDraft:
		if v.TestedAt != nil || v.TestedBy != "" || v.PublishedAt != nil || v.PublishedBy != "" || v.RetiredAt != nil || v.RetiredBy != "" {
			add("state", "draft cannot carry test, publish, or retire metadata")
		}
	case StateTesting:
		if v.TestedAt == nil || !validDisplay(v.TestedBy, maxActorLen) || v.PublishedAt != nil || v.PublishedBy != "" || v.RetiredAt != nil || v.RetiredBy != "" {
			add("state", "testing requires tester metadata only")
		}
	case StatePublished:
		if v.PublishedAt == nil || !validDisplay(v.PublishedBy, maxActorLen) || v.RetiredAt != nil || v.RetiredBy != "" {
			add("state", "published requires publisher metadata")
		}
	case StateRetired:
		if v.PublishedAt == nil || !validDisplay(v.PublishedBy, maxActorLen) || v.RetiredAt == nil || !validDisplay(v.RetiredBy, maxActorLen) {
			add("state", "retired requires publisher and retire metadata")
		}
	default:
		add("state", "must be draft, testing, published, or retired")
	}
	return fields
}

func expectString(obj map[string]json.RawMessage, key string, fields *[]FieldError) {
	raw, ok := obj[key]
	if !ok {
		return
	}
	var text string
	if json.Unmarshal(raw, &text) != nil {
		*fields = append(*fields, FieldError{Field: key, Message: "must be a string"})
	}
}

func expectObject(obj map[string]json.RawMessage, key string, fields *[]FieldError) {
	raw, ok := obj[key]
	if !ok {
		return
	}
	var doc map[string]json.RawMessage
	if json.Unmarshal(raw, &doc) != nil || doc == nil {
		*fields = append(*fields, FieldError{Field: key, Message: "must be a JSON object"})
	}
}

func expectStringArray(obj map[string]json.RawMessage, key string, fields *[]FieldError) {
	raw, ok := obj[key]
	if !ok {
		return
	}
	var items []string
	if json.Unmarshal(raw, &items) != nil {
		*fields = append(*fields, FieldError{Field: key, Message: "must be an array of strings"})
	}
}

func isJSONNumber(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return false
	}
	switch raw[0] {
	case '"', '{', '[', 't', 'f', 'n':
		return false
	}
	var number json.Number
	return json.Unmarshal(raw, &number) == nil
}

func unknownFieldName(err error) (string, bool) {
	const prefix = `json: unknown field "`
	message := err.Error()
	if strings.HasPrefix(message, prefix) && strings.HasSuffix(message, `"`) {
		return strings.TrimSuffix(strings.TrimPrefix(message, prefix), `"`), true
	}
	return "", false
}

func decodeCanonical(raw []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}
