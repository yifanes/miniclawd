package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os/exec"
	"sync"
	"time"

	"github.com/yifanes/miniclawd/internal/core"
)

// McpServer represents a configured MCP server.
type McpServer struct {
	Name      string
	Transport string // "stdio" or "http"
	Command   string // for stdio
	Args      []string
	URL       string // for http

	// Runtime state for stdio.
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Scanner
	mu     sync.Mutex
	nextID int

	// Cached tools.
	tools     []core.ToolDefinition
	toolsOnce sync.Once
}

// McpManager manages multiple MCP servers.
type McpManager struct {
	servers map[string]*McpServer
}

// McpServerConfig describes an MCP server in config.
type McpServerConfig struct {
	Name      string   `yaml:"name"`
	Transport string   `yaml:"transport"` // "stdio" or "http"
	Command   string   `yaml:"command"`
	Args      []string `yaml:"args"`
	URL       string   `yaml:"url"`
}

// NewMcpManager creates a manager from config.
func NewMcpManager(configs []McpServerConfig) *McpManager {
	m := &McpManager{servers: make(map[string]*McpServer)}
	for _, cfg := range configs {
		server := &McpServer{
			Name:      cfg.Name,
			Transport: cfg.Transport,
			Command:   cfg.Command,
			Args:      cfg.Args,
			URL:       cfg.URL,
		}
		m.servers[cfg.Name] = server
	}
	return m
}

// Initialize starts all servers and discovers tools.
func (m *McpManager) Initialize(ctx context.Context) error {
	for name, server := range m.servers {
		if err := server.start(ctx); err != nil {
			log.Printf("[mcp] failed to start server %q: %v", name, err)
			continue
		}
		if err := server.initialize(ctx); err != nil {
			log.Printf("[mcp] failed to initialize server %q: %v", name, err)
		}
	}
	return nil
}

// GetAllTools returns tool definitions from all MCP servers.
func (m *McpManager) GetAllTools() []core.ToolDefinition {
	var allTools []core.ToolDefinition
	for _, server := range m.servers {
		allTools = append(allTools, server.tools...)
	}
	return allTools
}

// CallTool dispatches a tool call to the appropriate server.
func (m *McpManager) CallTool(ctx context.Context, serverName, toolName string, input json.RawMessage) (string, error) {
	server, ok := m.servers[serverName]
	if !ok {
		return "", fmt.Errorf("unknown MCP server: %s", serverName)
	}
	return server.callTool(ctx, toolName, input)
}

// ServerNames returns the names of all configured servers.
func (m *McpManager) ServerNames() []string {
	var names []string
	for n := range m.servers {
		names = append(names, n)
	}
	return names
}

// Server returns a server by name.
func (m *McpManager) Server(name string) *McpServer {
	return m.servers[name]
}

// --- MCP Server methods ---

func (s *McpServer) start(ctx context.Context) error {
	if s.Transport == "http" {
		return nil // No process to start.
	}

	cmd := exec.CommandContext(ctx, s.Command, s.Args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	s.cmd = cmd
	s.stdin = stdin
	s.stdout = bufio.NewScanner(stdout)
	s.stdout.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	return nil
}

func (s *McpServer) initialize(ctx context.Context) error {
	// Send initialize request.
	resp, err := s.jsonRPC(ctx, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]string{
			"name":    "miniclawd",
			"version": "0.1.0",
		},
	})
	if err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	_ = resp

	// Discover tools.
	toolsResp, err := s.jsonRPC(ctx, "tools/list", nil)
	if err != nil {
		return fmt.Errorf("tools/list: %w", err)
	}

	var toolList struct {
		Tools []struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			InputSchema json.RawMessage `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(toolsResp, &toolList); err != nil {
		return fmt.Errorf("parsing tools: %w", err)
	}

	for _, t := range toolList.Tools {
		s.tools = append(s.tools, core.ToolDefinition{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		})
	}

	log.Printf("[mcp] server %q: %d tools discovered", s.Name, len(s.tools))
	return nil
}

func (s *McpServer) callTool(ctx context.Context, toolName string, input json.RawMessage) (string, error) {
	resp, err := s.jsonRPC(ctx, "tools/call", map[string]any{
		"name":      toolName,
		"arguments": json.RawMessage(input),
	})
	if err != nil {
		return "", err
	}

	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return string(resp), nil
	}

	var texts []string
	for _, c := range result.Content {
		if c.Type == "text" {
			texts = append(texts, c.Text)
		}
	}
	return joinTexts(texts), nil
}

func (s *McpServer) jsonRPC(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if s.Transport == "http" {
		return s.jsonRPCHTTP(ctx, method, params)
	}
	return s.jsonRPCStdio(ctx, method, params)
}

func (s *McpServer) jsonRPCStdio(_ context.Context, method string, params any) (json.RawMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.nextID++
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      s.nextID,
		"method":  method,
	}
	if params != nil {
		req["params"] = params
	}

	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	if _, err := s.stdin.Write(append(reqBytes, '\n')); err != nil {
		return nil, fmt.Errorf("write to MCP: %w", err)
	}

	if !s.stdout.Scan() {
		return nil, fmt.Errorf("no response from MCP server")
	}

	var resp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(s.stdout.Bytes(), &resp); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("MCP error %d: %s", resp.Error.Code, resp.Error.Message)
	}

	return resp.Result, nil
}

func (s *McpServer) jsonRPCHTTP(ctx context.Context, method string, params any) (json.RawMessage, error) {
	s.mu.Lock()
	s.nextID++
	id := s.nextID
	s.mu.Unlock()

	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
	}
	if params != nil {
		req["params"] = params
	}

	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", s.URL, bytes.NewReader(reqBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	httpResp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()

	var resp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("MCP error %d: %s", resp.Error.Code, resp.Error.Message)
	}

	return resp.Result, nil
}

// Close terminates all server processes.
func (m *McpManager) Close() {
	for _, server := range m.servers {
		if server.cmd != nil && server.cmd.Process != nil {
			server.stdin.Close()
			server.cmd.Process.Kill()
		}
	}
}

func joinTexts(texts []string) string {
	if len(texts) == 0 {
		return ""
	}
	result := texts[0]
	for _, t := range texts[1:] {
		result += "\n" + t
	}
	return result
}
