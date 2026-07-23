package logger

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fatih/color"
)

// LogEntry represents a single JSON structured line in the log files.
type LogEntry struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Message   string `json:"message"`
}

// Logger handles writes to both local rotating files and CLI stdout.
type Logger struct {
	mu     sync.Mutex
	logDir string
}

// NewLogger creates and returns a Logger instance. It ensures the log directory exists.
func NewLogger(logDir string) *Logger {
	resolved := resolvePath(logDir)
	_ = os.MkdirAll(resolved, 0755)
	l := &Logger{logDir: resolved}
	l.PruneOldLogs(30)
	return l
}

// Log writes a message with a specific level to a project-specific daily log file and standard output.
func (l *Logger) Log(project, level, format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()

	msg := fmt.Sprintf(format, args...)
	timestamp := time.Now().Format(time.RFC3339)

	// Build CLI prefix and log text
	var colorFn func(a ...interface{}) string
	switch strings.ToLower(level) {
	case "success":
		colorFn = color.New(color.FgGreen).SprintFunc()
	case "failed", "error":
		colorFn = color.New(color.FgRed).SprintFunc()
	case "warning":
		colorFn = color.New(color.FgYellow).SprintFunc()
	default:
		colorFn = color.New(color.FgCyan).SprintFunc()
	}

	// Print to CLI stdout with project prefix
	prefix := fmt.Sprintf("[%s] [%s]", time.Now().Format("15:04:05"), project)
	switch strings.ToLower(level) {
	case "exception", "panic", "fatal":
		color.Red("--------------------------------------------------------------------------------")
		color.Red("💥 EXCEPTION IN TERMINAL [%s]: %s", project, msg)
		color.Red("--------------------------------------------------------------------------------")
	default:
		fmt.Printf("%s %s\n", color.New(color.FgHiBlack).Sprint(prefix), colorFn(msg))
	}

	// Write JSON to daily log file
	dateStr := time.Now().Format("2006-01-02")
	safeProjectName := sanitizeFilename(project)
	logFilename := fmt.Sprintf("%s-%s.log", safeProjectName, dateStr)
	logPath := filepath.Join(l.logDir, logFilename)

	entry := LogEntry{
		Timestamp: timestamp,
		Level:     strings.ToUpper(level),
		Message:   msg,
	}

	data, err := json.Marshal(entry)
	if err == nil {
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err == nil {
			_, _ = f.Write(append(data, '\n'))
			_ = f.Close()
		}
	}
}

// TailLines retrieves the last N log lines across all log files for a given project.
func (l *Logger) TailLines(project string, n int) ([]string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	safeProjectName := sanitizeFilename(project)
	pattern := filepath.Join(l.logDir, safeProjectName+"-*.log")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}

	if len(matches) == 0 {
		return []string{}, nil
	}

	// Sort files chronologically (due to YYYY-MM-DD pattern in names)
	sort.Strings(matches)

	var lines []string
	// Read files from newest to oldest until we have enough lines
	for i := len(matches) - 1; i >= 0; i-- {
		fileLines, err := readLines(matches[i])
		if err != nil {
			continue
		}

		// Prepend lines so they maintain chronological order
		if len(lines)+len(fileLines) >= n {
			needed := n - len(lines)
			lines = append(fileLines[len(fileLines)-needed:], lines...)
			break
		} else {
			lines = append(fileLines, lines...)
		}
	}

	return lines, nil
}

// PruneOldLogs deletes any log files in the directory older than the specified number of days.
func (l *Logger) PruneOldLogs(days int) {
	l.mu.Lock()
	defer l.mu.Unlock()

	files, err := os.ReadDir(l.logDir)
	if err != nil {
		return
	}

	cutoff := time.Now().AddDate(0, 0, -days)
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".log") {
			continue
		}

		info, err := file.Info()
		if err != nil {
			continue
		}

		if info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(l.logDir, file.Name()))
		}
	}
}

// GetLogFilesForProject lists absolute paths to log files for a project, sorted by date.
func (l *Logger) GetLogFilesForProject(project string) ([]string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	safeProjectName := sanitizeFilename(project)
	pattern := filepath.Join(l.logDir, safeProjectName+"-*.log")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	return matches, nil
}

func readLines(filePath string) ([]string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	reader := bufio.NewReader(f)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				if len(line) > 0 {
					lines = append(lines, strings.TrimSuffix(line, "\n"))
				}
				break
			}
			return nil, err
		}
		lines = append(lines, strings.TrimSuffix(line, "\n"))
	}
	return lines, nil
}

func resolvePath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func sanitizeFilename(name string) string {
	// Replaces spaces and non-alphanumeric chars for safety
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, name)
}

// LogException formats runtime errors and panics with full colorized stack trace output to terminal.
func (l *Logger) LogException(project string, r interface{}, stackTrace string) {
	errStr := fmt.Sprintf("%v", r)
	color.Red("\n================================================================================")
	color.Red("  🚨 CODEFORGE TERMINAL EXCEPTION CAUGHT")
	color.Red("  Project: %s", project)
	color.Red("  Error:   %s", errStr)
	color.Red("--------------------------------------------------------------------------------")
	if stackTrace != "" {
		color.Yellow("%s", stackTrace)
	}
	color.Red("================================================================================\n")

	l.Log(project, "EXCEPTION", "Runtime Panic: %s\nStack:\n%s", errStr, stackTrace)
}

