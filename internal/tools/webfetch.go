package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/yifanes/miniclawd/internal/core"
)

const maxFetchBytes = 20 * 1024

type WebFetchTool struct{}

func NewWebFetchTool() *WebFetchTool { return &WebFetchTool{} }

func (t *WebFetchTool) Name() string { return "web_fetch" }

func (t *WebFetchTool) Definition() core.ToolDefinition {
	return MakeDef("web_fetch",
		"Fetch a URL and return its text content. HTML is converted to plain text. Max 20KB returned.",
		map[string]any{
			"url": StringProp("The URL to fetch"),
		},
		[]string{"url"},
	)
}

func (t *WebFetchTool) Execute(ctx context.Context, input json.RawMessage) ToolResult {
	var params struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return Error("invalid input: " + err.Error())
	}
	if params.URL == "" {
		return Error("url is required")
	}

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", params.URL, nil)
	if err != nil {
		return Error(fmt.Sprintf("invalid url: %v", err))
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; MiniClawd/1.0)")

	resp, err := client.Do(req)
	if err != nil {
		return Error(fmt.Sprintf("fetch error: %v", err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return Error(fmt.Sprintf("HTTP %d: %s", resp.StatusCode, resp.Status))
	}

	ct := resp.Header.Get("Content-Type")
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024)) // 1MB read limit
	if err != nil {
		return Error(fmt.Sprintf("read error: %v", err))
	}

	var text string
	if strings.Contains(ct, "text/html") {
		text = htmlToText(string(body))
	} else {
		text = string(body)
	}

	if len(text) > maxFetchBytes {
		text = text[:maxFetchBytes] + "\n... (content truncated)"
	}

	return Success(text)
}

func htmlToText(html string) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return html
	}
	// Remove scripts and styles.
	doc.Find("script, style, noscript").Remove()
	return strings.TrimSpace(doc.Text())
}
