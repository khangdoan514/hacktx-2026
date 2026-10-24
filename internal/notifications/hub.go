package notifications

import (
	"context"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/khangdoan514/hacktx-2026/internal/platform"
)

type Hub struct {
	mu   sync.Mutex
	subs map[string]map[chan string]struct{}
}

func NewHub() *Hub {
	return &Hub{subs: make(map[string]map[chan string]struct{})}
}

func (h *Hub) Publish(userID, message string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for channel := range h.subs[userID] {
		select {
		case channel <- message:
		default:
		}
	}
}

func (h *Hub) Subscribe(userID string) (<-chan string, func()) {
	channel := make(chan string, 8)
	h.mu.Lock()
	if h.subs[userID] == nil {
		h.subs[userID] = map[chan string]struct{}{}
	}
	h.subs[userID][channel] = struct{}{}
	h.mu.Unlock()
	return channel, func() {
		h.mu.Lock()
		delete(h.subs[userID], channel)
		h.mu.Unlock()
		close(channel)
	}
}

func Notify(ctx context.Context, tx pgx.Tx, hub *Hub, orgID, userID, action, resourceType, resourceID, message string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO app.employee_notifications (organization_id, user_id, action, resource_type, resource_id, message)
		VALUES ($1,$2,$3,$4,NULLIF($5,'')::uuid,$6)
	`, orgID, userID, action, resourceType, resourceID, message)
	if err == nil && hub != nil {
		hub.Publish(userID, message)
	}
	return err
}

func List(ctx context.Context, tx pgx.Tx, actor platform.Actor) ([]map[string]any, error) {
	rows, err := tx.Query(ctx, `
		SELECT id::text, action, message, created_at::text, read_at IS NULL
		FROM app.employee_notifications
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT 50
	`, actor.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, action, message, created string
		var unread bool
		if err := rows.Scan(&id, &action, &message, &created, &unread); err != nil {
			return nil, err
		}
		items = append(items, map[string]any{"id": id, "action": action, "message": message, "created_at": created, "unread": unread})
	}
	return items, rows.Err()
}
