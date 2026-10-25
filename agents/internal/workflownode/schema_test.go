package workflownode

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateSchemaSubset(t *testing.T) {
	schema := json.RawMessage(`{
		"type":"object",
		"properties":{
			"task":{"type":"string"},
			"count":{"type":"integer"},
			"tags":{"type":"array","items":{"type":"string","enum":["a","b"]}}
		},
		"required":["task"],
		"additionalProperties":false
	}`)
	cases := []struct {
		name  string
		value string
		ok    bool
		text  string
	}{
		{name: "valid", value: `{"task":"ship","count":2,"tags":["a"]}`, ok: true},
		{name: "missing required", value: `{"count":2}`, text: "missing task"},
		{name: "additional property", value: `{"task":"ship","extra":true}`, text: "unknown property extra"},
		{name: "wrong type", value: `{"task":1}`, text: "expected string"},
		{name: "integer rejects fraction", value: `{"task":"ship","count":1.5}`, text: "expected integer"},
		{name: "item enum", value: `{"task":"ship","tags":["z"]}`, text: "outside enum"},
		{name: "not json", value: `{`, text: "JSON"},
		{name: "empty", value: ``, text: "JSON"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateSchema(schema, json.RawMessage(tc.value))
			if tc.ok {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.text) {
				t.Fatalf("error %v, want text %q", err, tc.text)
			}
		})
	}
}

func TestValidateSchemaDocumentRejectsUnknownKeywords(t *testing.T) {
	err := ValidateSchemaDocument(json.RawMessage(`{"type":"object","minLength":1}`))
	if err == nil || !strings.Contains(err.Error(), "minLength") {
		t.Fatal(err)
	}
	if err := ValidateSchema(nil, json.RawMessage(`{"any":true}`)); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSchema(json.RawMessage(`null`), json.RawMessage(`[]`)); err != nil {
		t.Fatal(err)
	}
}

func TestUnmarshalArtifactRefRejectsInlinePayload(t *testing.T) {
	ref, err := UnmarshalArtifactRef([]byte(`{"id":"art-1","object_key":"tenant/run/art-1","content_type":"text/plain","byte_size":4}`))
	if err != nil {
		t.Fatal(err)
	}
	if ref.ID != "art-1" || ref.ObjectKey != "tenant/run/art-1" {
		t.Fatalf("%+v", ref)
	}
	for _, body := range []string{
		`{"id":"art-1","object_key":"k","data":"inline"}`,
		`{"id":"art-1","object_key":"k","secret":"tok"}`,
		`{"id":"","object_key":"k"}`,
		`{"id":"art-1","object_key":"k","byte_size":-1}`,
		`{"id":"art-1","object_key":"k"}{"id":"art-2","object_key":"k2"}`,
	} {
		if _, err := UnmarshalArtifactRef([]byte(body)); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
}
