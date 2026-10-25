package workflownode

import (
	"bytes"
	"encoding/json"
	"io"
)

// UnmarshalArtifactRef decodes one reference and rejects unknown fields so an
// inline payload or secret cannot ride along with the reference.
func UnmarshalArtifactRef(data []byte) (ArtifactRef, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var ref ArtifactRef
	if err := dec.Decode(&ref); err != nil {
		return ArtifactRef{}, &Error{Code: CodeInvalidInput, Message: "artifact reference is not a reference object"}
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return ArtifactRef{}, &Error{Code: CodeInvalidInput, Message: "artifact reference must be one JSON object"}
	}
	if err := validateArtifact(ref); err != nil {
		return ArtifactRef{}, err
	}
	return ref, nil
}

func validateArtifacts(refs []ArtifactRef) error {
	seen := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		if err := validateArtifact(ref); err != nil {
			return err
		}
		if _, ok := seen[ref.ID]; ok {
			return &Error{Code: CodeInvalidInput, Message: "duplicate artifact id"}
		}
		seen[ref.ID] = struct{}{}
	}
	return nil
}

func validateArtifact(ref ArtifactRef) error {
	if ref.ID == "" || ref.ObjectKey == "" {
		return &Error{Code: CodeInvalidInput, Message: "artifact reference requires an id and object key"}
	}
	if ref.ByteSize < 0 {
		return &Error{Code: CodeInvalidInput, Message: "artifact reference has a negative size"}
	}
	return nil
}
