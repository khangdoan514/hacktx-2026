package toolpacks

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
)

const githubAccept = "application/vnd.github+json"

var githubHeaders = map[string]string{"Accept": githubAccept, "X-GitHub-Api-Version": "2022-11-28"}

// Login verifies a token and returns the account it belongs to.
func (r *Runner) GitHubLogin(ctx context.Context, token string) (string, error) {
	var me struct {
		Login string `json:"login"`
	}
	if err := r.getJSON(ctx, r.GitHubAPI+"/user", token, githubHeaders, &me); err != nil {
		return "", err
	}
	return me.Login, nil
}

func (r *Runner) github(ctx context.Context, cred Credential, tool string, a arguments) (any, error) {
	tok := cred.AccessToken
	switch tool {
	case "search_repositories":
		if err := a.require("query"); err != nil {
			return nil, err
		}
		var out struct {
			Total int `json:"total_count"`
			Items []struct {
				FullName    string `json:"full_name"`
				Description string `json:"description"`
				Stars       int    `json:"stargazers_count"`
				Language    string `json:"language"`
				URL         string `json:"html_url"`
				Private     bool   `json:"private"`
			} `json:"items"`
		}
		q := url.Values{"q": {a.str("query")}, "per_page": {fmt.Sprint(a.limit())}}
		if err := r.getJSON(ctx, r.GitHubAPI+"/search/repositories?"+q.Encode(), tok, githubHeaders, &out); err != nil {
			return nil, err
		}
		return map[string]any{"total": out.Total, "repositories": out.Items}, nil
	case "search_code":
		if err := a.require("query"); err != nil {
			return nil, err
		}
		var out struct {
			Total int `json:"total_count"`
			Items []struct {
				Path       string `json:"path"`
				URL        string `json:"html_url"`
				Repository struct {
					FullName string `json:"full_name"`
				} `json:"repository"`
			} `json:"items"`
		}
		q := url.Values{"q": {a.str("query")}, "per_page": {fmt.Sprint(a.limit())}}
		if err := r.getJSON(ctx, r.GitHubAPI+"/search/code?"+q.Encode(), tok, githubHeaders, &out); err != nil {
			return nil, err
		}
		results := make([]map[string]string, 0, len(out.Items))
		for _, it := range out.Items {
			results = append(results, map[string]string{"repository": it.Repository.FullName, "path": it.Path, "url": it.URL})
		}
		return map[string]any{"total": out.Total, "results": results}, nil
	case "get_file":
		if err := a.require("owner", "repo", "path"); err != nil {
			return nil, err
		}
		u := fmt.Sprintf("%s/repos/%s/%s/contents/%s", r.GitHubAPI, esc(a.str("owner")), esc(a.str("repo")), escPath(a.str("path")))
		if ref := a.str("ref"); ref != "" {
			u += "?ref=" + url.QueryEscape(ref)
		}
		var out struct {
			Type     string `json:"type"`
			Path     string `json:"path"`
			Size     int    `json:"size"`
			Content  string `json:"content"`
			Encoding string `json:"encoding"`
			URL      string `json:"html_url"`
		}
		if err := r.getJSON(ctx, u, tok, githubHeaders, &out); err != nil {
			return nil, err
		}
		if out.Type != "file" || out.Encoding != "base64" {
			return nil, fmt.Errorf("%s is not a readable file (type %q)", a.str("path"), out.Type)
		}
		raw, err := base64.StdEncoding.DecodeString(stripNewlines(out.Content))
		if err != nil {
			return nil, fmt.Errorf("file content could not be decoded")
		}
		text, cut := truncate(string(raw))
		return map[string]any{"path": out.Path, "size": out.Size, "url": out.URL, "truncated": cut, "content": text}, nil
	case "list_issues", "list_pull_requests":
		if err := a.require("owner", "repo"); err != nil {
			return nil, err
		}
		state := a.str("state")
		if state != "closed" && state != "all" {
			state = "open"
		}
		kind := "issues"
		if tool == "list_pull_requests" {
			kind = "pulls"
		}
		u := fmt.Sprintf("%s/repos/%s/%s/%s?state=%s&per_page=%d", r.GitHubAPI, esc(a.str("owner")), esc(a.str("repo")), kind, state, a.limit())
		var out []struct {
			Number      int                    `json:"number"`
			Title       string                 `json:"title"`
			State       string                 `json:"state"`
			URL         string                 `json:"html_url"`
			User        struct{ Login string } `json:"user"`
			PullRequest *struct{}              `json:"pull_request"`
		}
		if err := r.getJSON(ctx, u, tok, githubHeaders, &out); err != nil {
			return nil, err
		}
		items := make([]map[string]any, 0, len(out))
		for _, it := range out {
			if kind == "issues" && it.PullRequest != nil {
				continue // the issues API also returns pull requests
			}
			items = append(items, map[string]any{"number": it.Number, "title": it.Title, "state": it.State, "author": it.User.Login, "url": it.URL})
		}
		return map[string]any{kind: items}, nil
	case "get_issue":
		if err := a.require("owner", "repo", "number"); err != nil {
			return nil, err
		}
		u := fmt.Sprintf("%s/repos/%s/%s/issues/%d", r.GitHubAPI, esc(a.str("owner")), esc(a.str("repo")), a.number("number"))
		var out struct {
			Number int                     `json:"number"`
			Title  string                  `json:"title"`
			State  string                  `json:"state"`
			Body   string                  `json:"body"`
			URL    string                  `json:"html_url"`
			User   struct{ Login string }  `json:"user"`
			Labels []struct{ Name string } `json:"labels"`
		}
		if err := r.getJSON(ctx, u, tok, githubHeaders, &out); err != nil {
			return nil, err
		}
		body, cut := truncate(out.Body)
		labels := make([]string, 0, len(out.Labels))
		for _, l := range out.Labels {
			labels = append(labels, l.Name)
		}
		return map[string]any{"number": out.Number, "title": out.Title, "state": out.State, "author": out.User.Login, "labels": labels, "url": out.URL, "truncated": cut, "body": body}, nil
	}
	return nil, fmt.Errorf("unknown tool github.%s", tool)
}
