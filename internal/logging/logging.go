package logging

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	filePrefix   = "miniclawd-"
	fileSuffix   = ".log"
	timeFormat   = "2006-01-02-15" // hourly rotation
	maxAgeDays   = 30
)

// HourlyLogWriter implements io.Writer with hourly log file rotation.
type HourlyLogWriter struct {
	mu      sync.Mutex
	dir     string
	current *os.File
	hour    string // current hour key (YYYY-MM-DD-HH)
}

// NewHourlyLogWriter creates a new hourly-rotating log writer.
func NewHourlyLogWriter(dir string) (*HourlyLogWriter, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating log directory %s: %w", dir, err)
	}
	return &HourlyLogWriter{dir: dir}, nil
}

// Write implements io.Writer. It rotates the log file when the hour changes.
func (w *HourlyLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	now := time.Now()
	hour := now.Format(timeFormat)

	if hour != w.hour {
		if err := w.rotate(hour); err != nil {
			return 0, err
		}
	}

	return w.current.Write(p)
}

// Close closes the current log file.
func (w *HourlyLogWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.current != nil {
		return w.current.Close()
	}
	return nil
}

func (w *HourlyLogWriter) rotate(hour string) error {
	if w.current != nil {
		w.current.Close()
	}

	filename := filepath.Join(w.dir, filePrefix+hour+fileSuffix)
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("opening log file %s: %w", filename, err)
	}

	w.current = f
	w.hour = hour

	// Clean up old logs on rotation.
	go w.cleanup()

	return nil
}

func (w *HourlyLogWriter) cleanup() {
	cutoff := time.Now().AddDate(0, 0, -maxAgeDays)
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, filePrefix) || !strings.HasSuffix(name, fileSuffix) {
			continue
		}
		// Parse hour from filename.
		hourStr := strings.TrimPrefix(name, filePrefix)
		hourStr = strings.TrimSuffix(hourStr, fileSuffix)
		t, err := time.Parse(timeFormat, hourStr)
		if err != nil {
			continue
		}
		if t.Before(cutoff) {
			os.Remove(filepath.Join(w.dir, name))
		}
	}
}

// InitFileLogging sets up file-based logging with hourly rotation.
func InitFileLogging(logsDir string) error {
	w, err := NewHourlyLogWriter(logsDir)
	if err != nil {
		return err
	}
	// Write to both file and stderr so early startup errors are still visible.
	multi := io.MultiWriter(os.Stderr, w)
	log.SetOutput(multi)
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	return nil
}

// InitConsoleLogging sets up standard console logging (default behavior).
func InitConsoleLogging() {
	log.SetOutput(os.Stderr)
	log.SetFlags(log.LstdFlags)
}

// ReadRecentLogs reads the last N lines from the most recent log files.
func ReadRecentLogs(logsDir string, lines int) (string, error) {
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		return "", fmt.Errorf("reading log directory: %w", err)
	}

	// Collect log files and sort by name (chronological due to naming scheme).
	var logFiles []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, filePrefix) && strings.HasSuffix(name, fileSuffix) {
			logFiles = append(logFiles, filepath.Join(logsDir, name))
		}
	}
	sort.Strings(logFiles)

	if len(logFiles) == 0 {
		return "No log files found.\n", nil
	}

	// Read lines from files in reverse order until we have enough.
	var allLines []string
	for i := len(logFiles) - 1; i >= 0 && len(allLines) < lines; i-- {
		fileLines, err := readFileLines(logFiles[i])
		if err != nil {
			continue
		}
		allLines = append(fileLines, allLines...)
	}

	// Trim to the requested number of lines.
	if len(allLines) > lines {
		allLines = allLines[len(allLines)-lines:]
	}

	return strings.Join(allLines, "\n") + "\n", nil
}

func readFileLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}
