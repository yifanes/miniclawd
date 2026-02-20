package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenAIEmbeddingProvider uses the OpenAI embeddings API (or compatible).
type OpenAIEmbeddingProvider struct {
	apiKey  string
	baseURL string
	model   string
	dim     int
	client  *http.Client
}

// NewOpenAIEmbeddingProvider creates an embedding provider.
func NewOpenAIEmbeddingProvider(apiKey, baseURL, model string, dim int) *OpenAIEmbeddingProvider {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1/embeddings"
	} else {
		baseURL = strings.TrimRight(baseURL, "/")
		if !strings.HasSuffix(baseURL, "/embeddings") {
			if strings.HasSuffix(baseURL, "/v1") {
				baseURL += "/embeddings"
			} else {
				baseURL += "/v1/embeddings"
			}
		}
	}
	if model == "" {
		model = "text-embedding-3-small"
	}
	if dim <= 0 {
		dim = 1536
	}
	return &OpenAIEmbeddingProvider{
		apiKey:  apiKey,
		baseURL: baseURL,
		model:   model,
		dim:     dim,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *OpenAIEmbeddingProvider) ModelName() string { return p.model }
func (p *OpenAIEmbeddingProvider) Dimension() int     { return p.dim }

func (p *OpenAIEmbeddingProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	body := map[string]any{
		"model": p.model,
		"input": texts,
	}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("embedding API error (status %d): %s", resp.StatusCode, string(errBody))
	}

	var result struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding embeddings: %w", err)
	}

	embeddings := make([][]float32, len(result.Data))
	for i, d := range result.Data {
		embeddings[i] = d.Embedding
	}
	return embeddings, nil
}
