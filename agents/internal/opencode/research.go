package opencode

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
)

type searchSource struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

type searchResult struct {
	Sources []searchSource `json:"sources"`
}

// recordSearch keeps only the query fields and public organic results, never
// headers, provider metadata, arbitrary parameters, or raw provider errors.
// V2 puts MCP text blocks in state.content; SerpApi wraps JSON in result.
func recordSearch(state map[string]any) (json.RawMessage, json.RawMessage, *contracts.RunError) {
	paramsInput := map[string]string{}
	input := map[string]any{"params": paramsInput}
	if args, ok := state["input"].(map[string]any); ok {
		if mode, ok := args["mode"].(string); ok && (mode == "compact" || mode == "complete") {
			input["mode"] = mode
		}
		if params, ok := args["params"].(map[string]any); ok {
			for _, key := range []string{"q", "engine", "location"} {
				if value, ok := params[key].(string); ok {
					paramsInput[key] = value
				}
			}
		}
	}
	inputJSON, _ := json.Marshal(input)
	failure := &contracts.RunError{Code: contracts.ErrInternal, Message: "SerpApi search failed or returned unreadable results; check credentials, quota, and connection"}
	if callFailed(state) {
		return inputJSON, nil, failure
	}
	content, _ := json.Marshal(state["content"])
	payload, err := searchPayload(content, 0)
	if err != nil {
		return inputJSON, nil, failure
	}
	var response struct {
		Error   string `json:"error"`
		Organic []struct {
			Title   string `json:"title"`
			Link    string `json:"link"`
			Snippet string `json:"snippet"`
		} `json:"organic_results"`
	}
	if json.Unmarshal(payload, &response) != nil || response.Error != "" {
		return inputJSON, nil, failure
	}
	result := searchResult{Sources: []searchSource{}}
	seen := map[string]bool{}
	for _, item := range response.Organic {
		parsed, err := url.Parse(item.Link)
		if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" || parsed.User != nil || seen[item.Link] {
			continue
		}
		seen[item.Link] = true
		result.Sources = append(result.Sources, searchSource{Title: item.Title, URL: item.Link, Snippet: item.Snippet})
	}
	output, _ := json.Marshal(result)
	return inputJSON, output, nil
}

// Unwrap MCP text and structuredContent/result envelopes. This only accepts
// JSON search responses, so an error or Markdown cannot become evidence.
func searchPayload(raw json.RawMessage, depth int) (json.RawMessage, error) {
	if depth > 8 {
		return nil, fmt.Errorf("search response nesting exceeded")
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	switch v := value.(type) {
	case string:
		return searchPayload(json.RawMessage(v), depth+1)
	case []any:
		for _, block := range v {
			data, _ := json.Marshal(block)
			if result, err := searchPayload(data, depth+1); err == nil {
				return result, nil
			}
		}
	case map[string]any:
		if isError, _ := v["isError"].(bool); isError {
			return nil, fmt.Errorf("MCP search error")
		}
		for _, key := range []string{"organic_results", "search_metadata", "search_information", "error"} {
			if _, ok := v[key]; ok {
				return raw, nil
			}
		}
		for _, key := range []string{"structuredContent", "result", "content", "text"} {
			if child, ok := v[key]; ok {
				data, _ := json.Marshal(child)
				if result, err := searchPayload(data, depth+1); err == nil {
					return result, nil
				}
			}
		}
	}
	return nil, fmt.Errorf("no JSON search response")
}

// Research must actually use the search tool. Resolve citations against the
// recorded results rather than trusting model-generated URLs, titles or snippets.
func researchOutput(output json.RawMessage, tools []contracts.ToolInvocation) (json.RawMessage, error) {
	observed := map[string]searchSource{}
	successful := false
	for _, tool := range tools {
		if tool.ToolID != "serpapi.search" || tool.Error != nil {
			continue
		}
		var result searchResult
		if json.Unmarshal(tool.Output, &result) != nil {
			continue
		}
		successful = true
		for _, source := range result.Sources {
			observed[source.URL] = source
		}
	}
	if !successful {
		return nil, &contracts.RunError{Code: contracts.ErrInternal, Message: "web research requires a successful SerpApi search; check credentials, quota, and connection"}
	}
	var result struct {
		Recommendation string   `json:"recommendation"`
		SourceIDs      []string `json:"source_ids"`
		Limitations    []string `json:"limitations"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, err
	}
	invalid := &contracts.RunError{Code: contracts.ErrOutputSchemaInvalid, Message: "research must cite URLs from the recorded search results"}
	if strings.TrimSpace(result.Recommendation) == "" || (len(observed) > 0 && len(result.SourceIDs) == 0) {
		return nil, invalid
	}
	sources := []searchSource{}
	for _, sourceID := range result.SourceIDs {
		source, ok := observed[sourceID]
		if !ok {
			return nil, invalid
		}
		sources = append(sources, source)
	}
	if len(observed) == 0 {
		result.Recommendation = "Search completed but returned no usable organic results."
	}
	result.Limitations = append(result.Limitations, "Based on search result snippets; full pages were not fetched.")
	return json.Marshal(struct {
		Recommendation string         `json:"recommendation"`
		SourceIDs      []string       `json:"source_ids"`
		Sources        []searchSource `json:"sources"`
		Limitations    []string       `json:"limitations"`
	}{result.Recommendation, result.SourceIDs, sources, result.Limitations})
}
