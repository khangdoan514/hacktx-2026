package context

import (
	"context"
	"strings"
)

// PreparedPrompt is what the coordinator may splice into a model prompt and
// what it must keep for authorization and inspection.
//
// Text is untrusted fragment text. It is the only model-facing field.
// AllowedTools is a copy of the allowlist on the request. SnapshotID is for
// an inspector. Denials, grant IDs, titles, and source URIs are not in Text.
type PreparedPrompt struct {
	Bundle       Bundle
	Text         string
	AllowedTools []string
	SnapshotID   string
}

// ResolveBeforePrompt resolves one invocation before the coordinator builds
// its model prompt. Call it for the depth-0 orchestrator. The coordinator
// supplies the pinned agent ID, depth, and allowlist. Model text must not be
// parsed into those fields.
func (r *Resolver) ResolveBeforePrompt(ctx context.Context, req ResolveRequest) (PreparedPrompt, error) {
	bundle, err := r.Resolve(ctx, req)
	if err != nil {
		return PreparedPrompt{}, err
	}
	return preparedPrompt(bundle), nil
}

// ForwardSpecialist is the specialist call. It reloads selected run artifacts
// under the specialist's own grants and returns that invocation's prompt text.
// The parent transcript, unselected artifacts, parent memory, organization
// hits, and parent allowlist are not copied. Depth 1 cannot forward again.
func (r *Resolver) ForwardSpecialist(ctx context.Context, req ForwardRequest) (PreparedPrompt, error) {
	bundle, err := r.Forward(ctx, req)
	if err != nil {
		return PreparedPrompt{}, err
	}
	return preparedPrompt(bundle), nil
}

func preparedPrompt(bundle Bundle) PreparedPrompt {
	return PreparedPrompt{
		Bundle:       bundle,
		Text:         fragmentPrompt(bundle),
		AllowedTools: cloneStrings(bundle.AllowedTools),
		SnapshotID:   bundle.SnapshotID,
	}
}

func fragmentPrompt(bundle Bundle) string {
	if len(bundle.Fragments) == 0 {
		return ""
	}
	parts := make([]string, len(bundle.Fragments))
	for i, fragment := range bundle.Fragments {
		parts[i] = fragment.Text
	}
	return strings.Join(parts, "\n")
}
