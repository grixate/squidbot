package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type WebSearchTool struct {
	apiKey     string
	maxResults int
	client     *http.Client
}

func NewWebSearchTool(apiKey string, maxResults int) *WebSearchTool {
	if maxResults <= 0 {
		maxResults = 5
	}
	return &WebSearchTool{apiKey: strings.TrimSpace(apiKey), maxResults: maxResults, client: &http.Client{Timeout: 15 * time.Second}}
}

func (t *WebSearchTool) Name() string { return "web_search" }
func (t *WebSearchTool) Description() string {
	return "Search the web. Returns titles, URLs, and snippets."
}
func (t *WebSearchTool) Schema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string"}, "count": map[string]any{"type": "integer", "minimum": 1, "maximum": 10}}, "required": []string{"query"}}
}
func (t *WebSearchTool) Execute(ctx context.Context, args json.RawMessage) (ToolResult, error) {
	var in struct {
		Query string `json:"query"`
		Count int    `json:"count"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return ToolResult{}, fmt.Errorf("invalid arguments: %w", err)
	}
	if strings.TrimSpace(t.apiKey) == "" {
		return ToolResult{Text: "Error: BRAVE_API_KEY not configured"}, nil
	}
	count := in.Count
	if count <= 0 {
		count = t.maxResults
	}
	if count > 10 {
		count = 10
	}

	endpoint := "https://api.search.brave.com/res/v1/web/search?q=" + url.QueryEscape(in.Query) + fmt.Sprintf("&count=%d", count)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ToolResult{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", t.apiKey)

	resp, err := t.client.Do(req)
	if err != nil {
		return ToolResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return ToolResult{Text: fmt.Sprintf("Error: search request failed (%d): %s", resp.StatusCode, string(body))}, nil
	}

	var parsed struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return ToolResult{}, err
	}
	if len(parsed.Web.Results) == 0 {
		return ToolResult{Text: "No results found."}, nil
	}

	lines := []string{fmt.Sprintf("Results for: %s", in.Query), ""}
	for idx, item := range parsed.Web.Results {
		lines = append(lines, fmt.Sprintf("%d. %s", idx+1, item.Title))
		lines = append(lines, "   "+item.URL)
		if strings.TrimSpace(item.Description) != "" {
			lines = append(lines, "   "+item.Description)
		}
	}
	return ToolResult{Text: strings.Join(lines, "\n")}, nil
}

type WebFetchTool struct {
	client   *http.Client
	maxChars int
}

func NewWebFetchTool(maxChars int) *WebFetchTool {
	if maxChars <= 0 {
		maxChars = 50000
	}
	return &WebFetchTool{client: &http.Client{Timeout: 30 * time.Second}, maxChars: maxChars}
}

func (t *WebFetchTool) Name() string { return "web_fetch" }
func (t *WebFetchTool) Description() string {
	return "Fetch URL and extract readable content (HTML to text/markdown)."
}
func (t *WebFetchTool) Schema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{"url": map[string]any{"type": "string"}, "extractMode": map[string]any{"type": "string", "enum": []string{"markdown", "text"}}, "maxChars": map[string]any{"type": "integer", "minimum": 100}}, "required": []string{"url"}}
}
func (t *WebFetchTool) Execute(ctx context.Context, args json.RawMessage) (ToolResult, error) {
	var in struct {
		URL         string `json:"url"`
		ExtractMode string `json:"extractMode"`
		MaxChars    int    `json:"maxChars"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return ToolResult{}, fmt.Errorf("invalid arguments: %w", err)
	}
	if strings.TrimSpace(in.URL) == "" {
		return ToolResult{}, fmt.Errorf("url required")
	}
	maxChars := in.MaxChars
	if maxChars <= 0 {
		maxChars = t.maxChars
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, in.URL, nil)
	if err != nil {
		return ToolResult{}, err
	}
	req.Header.Set("User-Agent", "squidbot/1.0")
	resp, err := t.client.Do(req)
	if err != nil {
		return ToolResult{}, err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxChars*4)))
	if err != nil {
		return ToolResult{}, err
	}
	body := string(bodyBytes)
	contentType := resp.Header.Get("Content-Type")

	text := body
	extractor := "raw"
	if strings.Contains(contentType, "application/json") {
		extractor = "json"
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, bodyBytes, "", "  "); err == nil {
			text = pretty.String()
		}
	} else if strings.Contains(contentType, "text/html") || looksLikeHTML(body) {
		extractor = "html"
		text = htmlToText(body)
	}

	truncated := false
	if len(text) > maxChars {
		text = text[:maxChars]
		truncated = true
	}
	result, _ := json.Marshal(map[string]any{
		"url":       in.URL,
		"status":    resp.StatusCode,
		"extractor": extractor,
		"truncated": truncated,
		"length":    len(text),
		"text":      text,
	})
	return ToolResult{Text: string(result)}, nil
}

func looksLikeHTML(content string) bool {
	head := strings.ToLower(content)
	if len(head) > 256 {
		head = head[:256]
	}
	return strings.Contains(head, "<html") || strings.Contains(head, "<!doctype")
}

func htmlToText(content string) string {
	re := regexp.MustCompile(`(?is)<(script|style)[^>]*>.*?</(script|style)>`)
	content = re.ReplaceAllString(content, "")
	re = regexp.MustCompile(`(?is)<br\s*/?>`)
	content = re.ReplaceAllString(content, "\n")
	re = regexp.MustCompile(`(?is)</(p|div|section|article|li|h1|h2|h3|h4|h5|h6)>`)
	content = re.ReplaceAllString(content, "\n")
	re = regexp.MustCompile(`(?is)<[^>]+>`)
	content = re.ReplaceAllString(content, "")
	re = regexp.MustCompile(`[ \t]+`)
	content = re.ReplaceAllString(content, " ")
	re = regexp.MustCompile(`\n{3,}`)
	content = re.ReplaceAllString(content, "\n\n")
	return strings.TrimSpace(content)
}
