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
	"strconv"
)

var slackChannelID = regexp.MustCompile(`^[CG][A-Z0-9]{2,32}$`)
var slackTimestamp = regexp.MustCompile(`^[0-9]{1,20}\.[0-9]{1,10}$`)

func (r *Runner) slackRequest(ctx context.Context, token, method, endpoint string, params map[string]any) (map[string]any, error) {
	var body io.Reader
	path := r.SlackAPI + "/" + endpoint
	if method == http.MethodGet {
		q := url.Values{}
		for key, value := range params {
			q.Set(key, fmt.Sprint(value))
		}
		path += "?" + q.Encode()
	} else {
		raw, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if method != http.MethodGet {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := r.Client.Do(req)
	if err != nil {
		return nil, errors.New("Slack could not be reached")
	}
	defer resp.Body.Close()
	if resp.StatusCode == 429 {
		seconds, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
		if seconds <= 0 {
			seconds = 60
		}
		return nil, fmt.Errorf("Slack rate limit reached. Retry after %d seconds", seconds)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("Slack returned HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (4<<20)+1))
	if err != nil || len(raw) > 4<<20 {
		return nil, errors.New("Slack response is too large or could not be read")
	}
	var out map[string]any
	if json.Unmarshal(raw, &out) != nil {
		return nil, errors.New("Slack returned an invalid response")
	}
	if ok, _ := out["ok"].(bool); !ok {
		code, _ := out["error"].(string)
		switch code {
		case "missing_scope":
			return nil, errors.New("Slack permissions are missing. Add the required User Token Scopes and reconnect from Tools")
		case "invalid_auth", "token_revoked", "token_expired", "account_inactive", "not_authed":
			return nil, errors.New("Slack authorization expired or was revoked. Reconnect Slack from Tools")
		case "channel_not_found", "not_in_channel", "thread_not_found":
			return nil, errors.New("The Slack channel or thread is unavailable to the connected account")
		case "ratelimited":
			return nil, errors.New("Slack rate limit reached. Try again shortly")
		case "feature_not_enabled", "enterprise_is_restricted", "not_allowed_token_type", "access_denied":
			return nil, errors.New("Slack has not enabled this capability for the connected user or app. Check AI search and workspace app permissions")
		default:
			return nil, errors.New("Slack refused the request. Check account permissions and app configuration")
		}
	}
	return out, nil
}

func (r *Runner) SlackAccount(ctx context.Context, token string) (string, error) {
	out, err := r.slackRequest(ctx, token, http.MethodPost, "auth.test", map[string]any{})
	if err != nil {
		return "", err
	}
	team, _ := out["team"].(string)
	user, _ := out["user"].(string)
	id, _ := out["user_id"].(string)
	if team == "" || id == "" {
		return "", errors.New("Slack did not return a workspace and user identity")
	}
	return team + " · " + firstNonEmpty(user, id), nil
}

func (r *Runner) slack(ctx context.Context, cred Credential, tool string, a arguments) (any, error) {
	params := map[string]any{"limit": a.limit()}
	if cursor := a.str("cursor"); cursor != "" {
		if len(cursor) > 512 {
			return nil, errors.New("cursor is too long")
		}
		params["cursor"] = cursor
	}
	endpoint, method := "", http.MethodGet
	switch tool {
	case "list_channels":
		endpoint = "conversations.list"
		params["types"] = "public_channel"
		params["exclude_archived"] = true
	case "search_messages":
		if err := a.require("query"); err != nil {
			return nil, err
		}
		if len(a.str("query")) > 4000 {
			return nil, errors.New("search query is too long")
		}
		endpoint, method = "assistant.search.context", http.MethodPost
		params["query"] = a.str("query")
		params["limit"] = min(a.limit(), 20)
		params["channel_types"] = []string{"public_channel"}
		params["content_types"] = []string{"messages"}
		params["include_context_messages"] = false
	case "get_channel_history", "get_thread", "get_permalink":
		channel := a.str("channel_id")
		if !slackChannelID.MatchString(channel) {
			return nil, errors.New("Use a Slack channel ID, not a name or URL")
		}
		params["channel"] = channel
		params["limit"] = min(a.limit(), 15)
		endpoint = "conversations.history"
		if tool == "get_thread" || tool == "get_permalink" {
			ts := a.str("ts")
			if !slackTimestamp.MatchString(ts) {
				return nil, errors.New("Use a Slack message timestamp such as 1700000000.000001")
			}
			if tool == "get_thread" {
				endpoint = "conversations.replies"
				params["ts"] = ts
			} else {
				endpoint = "chat.getPermalink"
				params = map[string]any{"channel": channel, "message_ts": ts}
			}
		}
	default:
		return nil, errors.New("unknown Slack tool")
	}
	out, err := r.slackRequest(ctx, cred.AccessToken, method, endpoint, params)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if tool == "get_permalink" {
		result = selectFields(out, "permalink", "channel")
	} else if tool == "list_channels" {
		channels := []any{}
		for _, item := range slackItems(out, "channels") {
			channels = append(channels, selectFields(item, "id", "name", "is_private", "is_member", "topic", "purpose"))
		}
		result = map[string]any{"channels": channels, "response_metadata": out["response_metadata"]}
	} else {
		source := out
		if tool == "search_messages" {
			source, _ = out["results"].(map[string]any)
		}
		messages := []any{}
		for _, item := range slackItems(source, "messages") {
			messages = append(messages, selectFields(item, "user", "text", "ts", "thread_ts", "reply_count", "channel_id", "author_user_id", "message_ts", "content", "permalink"))
		}
		result = map[string]any{"messages": messages, "has_more": out["has_more"], "response_metadata": out["response_metadata"]}
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	if len(raw) > maxText {
		return nil, errors.New("Slack result is too large. Use a smaller limit or narrower query")
	}
	return result, nil
}

func slackItems(out map[string]any, key string) []map[string]any {
	items, _ := out[key].([]any)
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if object, ok := item.(map[string]any); ok {
			result = append(result, object)
		}
	}
	return result
}
