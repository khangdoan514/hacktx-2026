package runtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/khangdoan514/hacktx-2026/internal/agents/contracts"
)

type Meta struct {
	OrganizationID  string
	ActorID         string
	ProjectID       string
	ExecutionMode   string
	AdapterVersion  string
	Limits          contracts.Limits
	CancelRequested bool
}

type metaKey struct{}

func WithMeta(ctx context.Context, meta Meta) context.Context {
	return context.WithValue(ctx, metaKey{}, meta)
}

func MetaFrom(ctx context.Context) (Meta, bool) {
	meta, ok := ctx.Value(metaKey{}).(Meta)
	return meta, ok
}

func newID(prefix string) string {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	return fmt.Sprintf("%s_%s", prefix, hex.EncodeToString(buf))
}
