package toolpacks

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

type driveFile struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	MimeType string `json:"mimeType"`
	Modified string `json:"modifiedTime"`
	URL      string `json:"webViewLink"`
}

const driveFields = "files(id,name,mimeType,modifiedTime,webViewLink)"

// DriveAccount verifies a token and returns the account email.
func (r *Runner) DriveAccount(ctx context.Context, token string) (string, error) {
	var out struct {
		User struct {
			Email string `json:"emailAddress"`
		} `json:"user"`
	}
	if err := r.getJSON(ctx, r.DriveAPI+"/about?fields=user(emailAddress)", token, nil, &out); err != nil {
		return "", err
	}
	return out.User.Email, nil
}

// googleExports maps native Google formats to a text export.
var googleExports = map[string]string{
	"application/vnd.google-apps.document":     "text/plain",
	"application/vnd.google-apps.spreadsheet":  "text/csv",
	"application/vnd.google-apps.presentation": "text/plain",
}

func (r *Runner) drive(ctx context.Context, cred Credential, tool string, a arguments) (any, error) {
	tok := cred.AccessToken
	switch tool {
	case "search_files":
		if err := a.require("query"); err != nil {
			return nil, err
		}
		q := fmt.Sprintf("fullText contains '%s' and trashed = false", driveQuote(a.str("query")))
		return r.driveList(ctx, tok, q, a.limit())
	case "list_folder":
		folder := a.str("folder_id")
		if folder == "" {
			folder = "root"
		}
		q := fmt.Sprintf("'%s' in parents and trashed = false", driveQuote(folder))
		return r.driveList(ctx, tok, q, a.limit())
	case "read_file":
		if err := a.require("file_id"); err != nil {
			return nil, err
		}
		id := esc(a.str("file_id"))
		var meta driveFile
		if err := r.getJSON(ctx, r.DriveAPI+"/files/"+id+"?fields=id,name,mimeType,modifiedTime,webViewLink,size", tok, nil, &meta); err != nil {
			return nil, err
		}
		var raw []byte
		var err error
		if export, ok := googleExports[meta.MimeType]; ok {
			raw, err = r.get(ctx, r.DriveAPI+"/files/"+id+"/export?mimeType="+url.QueryEscape(export), tok, nil)
		} else if strings.HasPrefix(meta.MimeType, "text/") || meta.MimeType == "application/json" {
			raw, err = r.get(ctx, r.DriveAPI+"/files/"+id+"?alt=media", tok, nil)
		} else {
			return nil, fmt.Errorf("%s (%s) is not a text file or Google document, so it cannot be read", meta.Name, meta.MimeType)
		}
		if err != nil {
			return nil, err
		}
		text, cut := truncate(string(raw))
		return map[string]any{"id": meta.ID, "name": meta.Name, "mime_type": meta.MimeType, "url": meta.URL, "truncated": cut, "content": text}, nil
	}
	return nil, fmt.Errorf("unknown tool gdrive.%s", tool)
}

func (r *Runner) driveList(ctx context.Context, token, q string, limit int) (any, error) {
	var out struct {
		Files []driveFile `json:"files"`
	}
	v := url.Values{"q": {q}, "pageSize": {fmt.Sprint(limit)}, "fields": {driveFields}, "orderBy": {"modifiedTime desc"}}
	if err := r.getJSON(ctx, r.DriveAPI+"/files?"+v.Encode(), token, nil, &out); err != nil {
		return nil, err
	}
	return map[string]any{"files": out.Files}, nil
}

// driveQuote escapes a value inside a single-quoted Drive query string.
func driveQuote(s string) string {
	return strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s)
}

func escPath(p string) string {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

func stripNewlines(s string) string {
	return strings.NewReplacer("\n", "", "\r", "").Replace(s)
}
