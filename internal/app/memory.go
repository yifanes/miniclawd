package app

import (
	"fmt"
	"os"
	"path/filepath"
)

// MemoryManager handles file-based AGENTS.md memory operations.
type MemoryManager struct {
	dataDir string
}

// NewMemoryManager creates a new MemoryManager.
func NewMemoryManager(dataDir string) *MemoryManager {
	return &MemoryManager{dataDir: dataDir}
}

// GlobalMemoryPath returns the path to the global AGENTS.md.
func (m *MemoryManager) GlobalMemoryPath() string {
	return filepath.Join(m.dataDir, "runtime", "groups", "AGENTS.md")
}

// ChatMemoryPath returns the path to a chat-specific AGENTS.md.
func (m *MemoryManager) ChatMemoryPath(chatID int64) string {
	return filepath.Join(m.dataDir, "runtime", "groups", fmt.Sprintf("%d", chatID), "AGENTS.md")
}

// ReadGlobalMemory reads the global memory file.
func (m *MemoryManager) ReadGlobalMemory() (string, error) {
	data, err := os.ReadFile(m.GlobalMemoryPath())
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

// ReadChatMemory reads a chat-specific memory file.
func (m *MemoryManager) ReadChatMemory(chatID int64) (string, error) {
	data, err := os.ReadFile(m.ChatMemoryPath(chatID))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

// WriteGlobalMemory writes the global memory file.
func (m *MemoryManager) WriteGlobalMemory(content string) error {
	path := m.GlobalMemoryPath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// WriteChatMemory writes a chat-specific memory file.
func (m *MemoryManager) WriteChatMemory(chatID int64, content string) error {
	path := m.ChatMemoryPath(chatID)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// EnsureDirectories creates required runtime directories.
func (m *MemoryManager) EnsureDirectories() error {
	dirs := []string{
		filepath.Join(m.dataDir, "runtime", "groups"),
		filepath.Join(m.dataDir, "exports"),
		filepath.Join(m.dataDir, "skills"),
		filepath.Join(m.dataDir, "hooks"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating directory %s: %w", dir, err)
		}
	}
	return nil
}
