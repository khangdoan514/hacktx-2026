package threads

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

func decodedAttachment(t *testing.T, uri string) string {
	t.Helper()
	_, encoded, ok := strings.Cut(uri, ",")
	if !ok {
		t.Fatal("missing data URL payload")
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestPromptAttachmentFormats(t *testing.T) {
	if a := promptAttachment("report.pdf", "application/octet-stream", []byte("%PDF-1.7\n")); !strings.HasPrefix(a.URI, "data:application/pdf;base64,") {
		t.Fatalf("pdf: %+v", a)
	}
	if a := promptAttachment("notes.txt", "application/octet-stream", []byte("analysis text")); decodedAttachment(t, a.URI) != "analysis text" {
		t.Fatal("text content lost")
	}
	if a := promptAttachment("blob.bin", "application/octet-stream", []byte{0, 1, 2}); !strings.Contains(decodedAttachment(t, a.URI), "cannot be decoded") {
		t.Fatal("binary unsupported note missing")
	}
	if got := inlineAttachmentText("alpha.txt", []byte("Alpha code: 19.")); got != "Alpha code: 19." {
		t.Fatalf("inline text = %q", got)
	}
	if got := inlineAttachmentText("report.pdf", []byte("%PDF-1.7\n")); got != "" {
		t.Fatalf("PDF should stay native: %q", got)
	}
}

func TestOfficeDocumentExtractsReadableText(t *testing.T) {
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	f, err := z.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.Write([]byte(`<w:document xmlns:w="urn:word"><w:t>Revenue</w:t><w:t>increased</w:t></w:document>`))
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	a := promptAttachment("report.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", buf.Bytes())
	got := decodedAttachment(t, a.URI)
	if !strings.Contains(got, "Revenue increased") {
		t.Fatalf("document text = %q", got)
	}
	if inline := inlineAttachmentText("report.docx", buf.Bytes()); !strings.Contains(inline, "Revenue increased") {
		t.Fatalf("inline document text = %q", inline)
	}
}
