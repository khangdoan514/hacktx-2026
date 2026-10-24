package platform

import "context"

type Actor struct {
	ID    string
	Email string
	OrgID string
	Roles []string
}

type actorKey struct{}

func WithActor(ctx context.Context, actor Actor) context.Context {
	return context.WithValue(ctx, actorKey{}, actor)
}

func ActorFrom(ctx context.Context) (Actor, bool) {
	actor, ok := ctx.Value(actorKey{}).(Actor)
	return actor, ok
}

func (a Actor) Has(role string) bool {
	for _, candidate := range a.Roles {
		if candidate == role {
			return true
		}
	}
	return false
}
