package toolpacks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// Pin the stable API that introduced data sources rather than deprecated
// database queries. Page bodies are read separately through block children.
const notionVersion = "2025-09-03"

var notionID = regexp.MustCompile(`^(?:[a-fA-F0-9]{32}|[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12})$`)

type notionHTTPError struct{ status int }

func (e notionHTTPError) Error() string {
	switch e.status {
	case 401:
		return "Notion authorization expired. Reconnect Notion from Tools."
	case 403:
		return "Notion access denied. Enable Read content and share the page with this connection."
	case 404:
		return "Notion page or data source was not found, or was not shared with this connection."
	case 429:
		return "Notion rate limit reached. Try again shortly."
	default:
		return fmt.Sprintf("Notion returned HTTP %d", e.status)
	}
}

func (r *Runner) notionRequest(ctx context.Context, token, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, r.NotionAPI+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Notion-Version", notionVersion)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := r.Client.Do(req)
	if err != nil {
		return errors.New("Notion could not be reached")
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return notionHTTPError{status: resp.StatusCode}
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (4<<20)+1))
	if err != nil {
		return errors.New("Notion response could not be read")
	}
	if len(raw) > 4<<20 {
		return errors.New("Notion response is too large; request fewer results")
	}
	if json.Unmarshal(raw, out) != nil {
		return errors.New("Notion returned an invalid response")
	}
	return nil
}

func (r *Runner) NotionAccount(ctx context.Context, token string) (string, error) {
	var out struct {
		Name string `json:"name"`
		Bot  struct {
			WorkspaceName string `json:"workspace_name"`
			Owner         struct {
				User struct {
					Name   string `json:"name"`
					Person struct {
						Email string `json:"email"`
					} `json:"person"`
				} `json:"user"`
			} `json:"owner"`
		} `json:"bot"`
	}
	if err := r.notionRequest(ctx, token, http.MethodGet, "/users/me", nil, &out); err != nil {
		return "", err
	}
	return firstNonEmpty(out.Bot.WorkspaceName, out.Bot.Owner.User.Person.Email, out.Bot.Owner.User.Name, out.Name, "Notion workspace"), nil
}

func (r *Runner) notion(ctx context.Context, cred Credential, tool string, args arguments) (any, error) {
	result, err := r.notionCall(ctx, cred.AccessToken, tool, args)
	var status notionHTTPError
	if errors.As(err, &status) && status.status == 401 && cred.Kind == "oauth" && cred.RefreshToken != "" {
		provider, _ := ProviderFor("notion")
		next, refreshErr := provider.Refresh(ctx, r.Client, cred)
		if refreshErr != nil {
			return nil, errors.New("Notion authorization expired. Reconnect Notion from Tools.")
		}
		if saveErr := r.Store.Put("notion", next); saveErr != nil {
			return nil, saveErr
		}
		return r.notionCall(ctx, next.AccessToken, tool, args)
	}
	return result, err
}

func (r *Runner) notionCall(ctx context.Context, token, tool string, a arguments) (any, error) {
	var path, method string
	var body any
	var id string
	page := map[string]any{"page_size": a.limit()}
	if cursor := a.str("cursor"); cursor != "" {
		if len(cursor) > 512 {
			return nil, errors.New("cursor is too long")
		}
		page["start_cursor"] = cursor
	}
	switch tool {
	case "search":
		path, method = "/search", http.MethodPost
		if query := a.str("query"); query != "" {
			page["query"] = query
		}
		page["sort"] = map[string]string{"direction": "descending", "timestamp": "last_edited_time"}
		body = page
	case "get_page":
		id = a.str("page_id")
		path, method = "/pages/"+id, http.MethodGet
	case "get_database":
		id = a.str("database_id")
		path, method = "/databases/"+id, http.MethodGet
	case "list_blocks":
		id = a.str("block_id")
		q := url.Values{"page_size": {fmt.Sprint(a.limit())}}
		if cursor := a.str("cursor"); cursor != "" {
			q.Set("start_cursor", cursor)
		}
		path, method = "/blocks/"+id+"/children?"+q.Encode(), http.MethodGet
	case "query_data_source":
		id = a.str("data_source_id")
		path, method, body = "/data_sources/"+id+"/query", http.MethodPost, page
	default:
		return nil, errors.New("unknown Notion tool")
	}
	if tool != "search" && !notionID.MatchString(id) {
		return nil, errors.New("Use a valid Notion UUID, not a URL or path")
	}
	var out map[string]any
	if err := r.notionRequest(ctx, token, method, path, body, &out); err != nil {
		return nil, err
	}
	if items, ok := out["results"].([]any); ok {
		results := make([]any, 0, len(items))
		for _, item := range items {
			object, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if tool == "list_blocks" {
				kind, _ := object["type"].(string)
				results = append(results, selectFields(object, "id", "type", "has_children", kind))
			} else {
				results = append(results, selectFields(object, "id", "object", "url", "title", "properties", "parent", "last_edited_time"))
			}
		}
		out = map[string]any{"results": results, "has_more": out["has_more"], "next_cursor": out["next_cursor"]}
	} else {
		out = selectFields(out, "id", "object", "url", "properties", "title", "description", "data_sources", "parent", "last_edited_time")
	}
	// Never return a cut JSON string as if it were a complete object.
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	if len(raw) > maxText {
		return nil, errors.New("Notion result is too large; lower the limit and read smaller sections")
	}
	return out, nil
}

func selectFields(in map[string]any, fields ...string) map[string]any {
	out := map[string]any{}
	for _, field := range fields {
		if value, ok := in[field]; ok && strings.TrimSpace(field) != "" {
			out[field] = value
		}
	}
	return out
}
