package clawhub

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ClawHubGateway is an HTTP client for the ClawHub registry API.
type ClawHubGateway struct {
	registryURL string
	token       string
	client      *http.Client
}

// NewClawHubGateway creates a new ClawHub client.
func NewClawHubGateway(registryURL, token string) *ClawHubGateway {
	return &ClawHubGateway{
		registryURL: strings.TrimRight(registryURL, "/"),
		token:       token,
		client:      &http.Client{Timeout: 30 * time.Second},
	}
}

// SkillInfo represents a skill in the registry.
type SkillInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version"`
	Author      string `json:"author"`
	Downloads   int    `json:"downloads"`
}

// Search searches the registry for skills matching a query.
func (g *ClawHubGateway) Search(ctx context.Context, query string) ([]SkillInfo, error) {
	u := fmt.Sprintf("%s/api/v1/skills/search?q=%s", g.registryURL, url.QueryEscape(query))

	data, err := g.get(ctx, u)
	if err != nil {
		return nil, err
	}

	var results []SkillInfo
	if err := json.Unmarshal(data, &results); err != nil {
		return nil, fmt.Errorf("parsing search results: %w", err)
	}
	return results, nil
}

// Download fetches a skill's SKILL.md content from the registry.
func (g *ClawHubGateway) Download(ctx context.Context, skillName, version string) ([]byte, error) {
	versionParam := ""
	if version != "" {
		versionParam = "?version=" + url.QueryEscape(version)
	}

	u := fmt.Sprintf("%s/api/v1/skills/%s/download%s", g.registryURL, url.PathEscape(skillName), versionParam)
	return g.get(ctx, u)
}

// GetInfo fetches metadata for a specific skill.
func (g *ClawHubGateway) GetInfo(ctx context.Context, skillName string) (*SkillInfo, error) {
	u := fmt.Sprintf("%s/api/v1/skills/%s", g.registryURL, url.PathEscape(skillName))

	data, err := g.get(ctx, u)
	if err != nil {
		return nil, err
	}

	var info SkillInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("parsing skill info: %w", err)
	}
	return &info, nil
}

func (g *ClawHubGateway) get(ctx context.Context, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	if g.token != "" {
		req.Header.Set("Authorization", "Bearer "+g.token)
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request error: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read error: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("registry error (status %d): %s", resp.StatusCode, string(body))
	}

	return body, nil
}
