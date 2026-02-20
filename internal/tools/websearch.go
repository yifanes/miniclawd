package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/yifanes/miniclawd/internal/core"
)

type WebSearchTool struct{}

func NewWebSearchTool() *WebSearchTool { return &WebSearchTool{} }

func (t *WebSearchTool) Name() string { return "web_search" }

func (t *WebSearchTool) Definition() core.ToolDefinition {
	return MakeDef("web_search",
		"Search the web using DuckDuckGo. Returns titles, URLs, and snippets.",
		map[string]any{
			"query": StringProp("The search query"),
		},
		[]string{"query"},
	)
}

func (t *WebSearchTool) Execute(ctx context.Context, input json.RawMessage) ToolResult {
	var params struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return Error("invalid input: " + err.Error())
	}
	if params.Query == "" {
		return Error("query is required")
	}

	results, err := searchDDG(ctx, params.Query)
	if err != nil {
		return Error(fmt.Sprintf("search error: %v", err))
	}

	if len(results) == 0 {
		return Success("No results found")
	}

	var sb strings.Builder
	for i, r := range results {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(fmt.Sprintf("[%d] %s\n%s\n%s", i+1, r.title, r.url, r.snippet))
	}
	return Success(sb.String())
}

type searchResult struct {
	title   string
	url     string
	snippet string
}

func searchDDG(ctx context.Context, query string) ([]searchResult, error) {
	u := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(query)

	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; MiniClawd/1.0)")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}

	var results []searchResult
	doc.Find(".result").Each(func(i int, s *goquery.Selection) {
		if i >= 10 {
			return
		}
		title := strings.TrimSpace(s.Find(".result__a").Text())
		link, _ := s.Find(".result__a").Attr("href")
		snippet := strings.TrimSpace(s.Find(".result__snippet").Text())

		if title != "" && link != "" {
			results = append(results, searchResult{
				title:   title,
				url:     link,
				snippet: snippet,
			})
		}
	})

	return results, nil
}
