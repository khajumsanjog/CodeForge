package progress

import (
	"context"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type progressKey struct{}

var (
	globalTrackers   = make(map[string]*Tracker)
	globalTrackersMu sync.RWMutex
)

// SetGlobalTracker registers a Tracker for a project.
func SetGlobalTracker(project string, tracker *Tracker) {
	globalTrackersMu.Lock()
	defer globalTrackersMu.Unlock()
	globalTrackers[project] = tracker
}

// GetGlobalTracker returns the registered Tracker for a project.
func GetGlobalTracker(project string) *Tracker {
	globalTrackersMu.RLock()
	defer globalTrackersMu.RUnlock()
	return globalTrackers[project]
}

// ClearGlobalTracker removes a project's Tracker from registry.
func ClearGlobalTracker(project string) {
	globalTrackersMu.Lock()
	defer globalTrackersMu.Unlock()
	delete(globalTrackers, project)
}

// GetAllActiveProgress returns snapshots of all currently running transfers.
func GetAllActiveProgress() map[string]Snapshot {
	globalTrackersMu.RLock()
	defer globalTrackersMu.RUnlock()

	res := make(map[string]Snapshot)
	for k, v := range globalTrackers {
		if v != nil {
			res[k] = v.GetSnapshot()
		}
	}
	return res
}

// WithTracker attaches a Tracker to context.
func WithTracker(ctx context.Context, tracker *Tracker) context.Context {
	return context.WithValue(ctx, progressKey{}, tracker)
}

// GetTracker retrieves a Tracker from context if available.
func GetTracker(ctx context.Context) *Tracker {
	if tracker, ok := ctx.Value(progressKey{}).(*Tracker); ok {
		return tracker
	}
	return nil
}

// Snapshot represents a point-in-time progress state.
type Snapshot struct {
	TotalFiles       int64         `json:"total_files"`
	TransferredFiles int64         `json:"transferred_files"`
	TotalBytes       int64         `json:"total_bytes"`
	TransferredBytes int64         `json:"transferred_bytes"`
	CurrentFile      string        `json:"current_file"`
	SpeedBytesPerSec float64       `json:"speed_bytes_per_sec"`
	Percentage       float64       `json:"percentage"`
	ETA              time.Duration `json:"eta"`
}

// ProgressCallback is invoked when progress updates.
type ProgressCallback func(snap Snapshot)

// Tracker coordinates progress metrics across operations.
type Tracker struct {
	mu               sync.RWMutex
	totalFiles       int64
	transferredFiles int64
	totalBytes       int64
	transferredBytes int64
	currentFile      string
	startTime        time.Time
	callbacks        []ProgressCallback
}

// NewTracker creates a new progress tracker.
func NewTracker(totalFiles int64, totalBytes int64) *Tracker {
	return &Tracker{
		totalFiles: totalFiles,
		totalBytes: totalBytes,
		startTime:  time.Now(),
	}
}

// RegisterCallback attaches an event listener for progress updates.
func (t *Tracker) RegisterCallback(cb ProgressCallback) {
	if cb == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.callbacks = append(t.callbacks, cb)
}

// SetTotalFiles updates the total number of files.
func (t *Tracker) SetTotalFiles(n int64) {
	t.mu.Lock()
	t.totalFiles = n
	t.mu.Unlock()
	t.notify()
}

// SetTotalBytes updates total size in bytes.
func (t *Tracker) SetTotalBytes(n int64) {
	t.mu.Lock()
	t.totalBytes = n
	t.mu.Unlock()
	t.notify()
}

// StartFile marks the beginning of a file transfer.
func (t *Tracker) StartFile(filename string) {
	t.mu.Lock()
	t.currentFile = filepath.Base(filename)
	t.mu.Unlock()
	t.notify()
}

// CompleteFile increments the finished file count.
func (t *Tracker) CompleteFile() {
	t.mu.Lock()
	t.transferredFiles++
	t.mu.Unlock()
	t.notify()
}

// AddBytes increments transferred byte count.
func (t *Tracker) AddBytes(n int64) {
	t.mu.Lock()
	t.transferredBytes += n
	t.mu.Unlock()
	t.notify()
}

// GetSnapshot calculates rates and estimates.
func (t *Tracker) GetSnapshot() Snapshot {
	t.mu.RLock()
	defer t.mu.RUnlock()

	elapsed := time.Since(t.startTime).Seconds()
	var speed float64
	if elapsed > 0 {
		speed = float64(t.transferredBytes) / elapsed
	}

	var pct float64
	if t.totalBytes > 0 {
		pct = (float64(t.transferredBytes) / float64(t.totalBytes)) * 100.0
	} else if t.totalFiles > 0 {
		pct = (float64(t.transferredFiles) / float64(t.totalFiles)) * 100.0
	}
	if pct > 100.0 {
		pct = 100.0
	}

	var eta time.Duration
	if speed > 0 && t.totalBytes > t.transferredBytes {
		remainingBytes := float64(t.totalBytes - t.transferredBytes)
		etaSec := remainingBytes / speed
		eta = time.Duration(math.Max(0, etaSec)) * time.Second
	}

	return Snapshot{
		TotalFiles:       t.totalFiles,
		TransferredFiles: t.transferredFiles,
		TotalBytes:       t.totalBytes,
		TransferredBytes: t.transferredBytes,
		CurrentFile:      t.currentFile,
		SpeedBytesPerSec: speed,
		Percentage:       pct,
		ETA:              eta,
	}
}

func (t *Tracker) notify() {
	snap := t.GetSnapshot()
	t.mu.RLock()
	cbs := append([]ProgressCallback(nil), t.callbacks...)
	t.mu.RUnlock()

	for _, cb := range cbs {
		cb(snap)
	}
}

// ProgressReader wraps an io.Reader to report bytes read to a Tracker.
type ProgressReader struct {
	reader  io.Reader
	tracker *Tracker
}

// NewProgressReader returns an io.Reader wrapper.
func NewProgressReader(r io.Reader, tracker *Tracker) *ProgressReader {
	return &ProgressReader{
		reader:  r,
		tracker: tracker,
	}
}

func (pr *ProgressReader) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)
	if n > 0 && pr.tracker != nil {
		pr.tracker.AddBytes(int64(n))
	}
	return n, err
}

// RenderCLIProgressBar formats a Progress Snapshot as a terminal ASCII bar.
func RenderCLIProgressBar(snap Snapshot, barWidth int) string {
	if barWidth <= 0 {
		barWidth = 24
	}

	filledLen := int(math.Round(float64(barWidth) * (snap.Percentage / 100.0)))
	if filledLen < 0 {
		filledLen = 0
	}
	if filledLen > barWidth {
		filledLen = barWidth
	}

	var bar string
	if filledLen > 0 {
		bar = strings.Repeat("=", filledLen-1) + ">"
	}
	if len(bar) < filledLen {
		bar = strings.Repeat("=", filledLen)
	}
	bar += strings.Repeat(" ", barWidth-len(bar))

	speedStr := FormatBytes(int64(snap.SpeedBytesPerSec)) + "/s"
	transferredStr := FormatBytes(snap.TransferredBytes)
	totalStr := FormatBytes(snap.TotalBytes)

	var filesInfo string
	if snap.TotalFiles > 0 {
		filesInfo = fmt.Sprintf(" | %d/%d files", snap.TransferredFiles, snap.TotalFiles)
	}

	return fmt.Sprintf("[%s] %5.1f%% (%s / %s%s @ %s)",
		bar, snap.Percentage, transferredStr, totalStr, filesInfo, speedStr)
}

// FormatBytes converts raw bytes into human readable format (KB, MB, GB).
func FormatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
