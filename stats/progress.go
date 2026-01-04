package stats

import (
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

// Stage represents the current stage of stats collection
type Stage string

const (
	StageAnalyzing       Stage = "analyzing"
	StageCollectingStats Stage = "collecting_stats"
	StageInferring       Stage = "inferring"
	StageCompleted       Stage = "completed"
	StageFailed          Stage = "failed"
	StageCancelled       Stage = "cancelled"
)

// Progress represents the current progress of stats collection
type Progress struct {
	Stage            Stage  `json:"stage"`
	CurrentTable     string `json:"current_table,omitempty"`
	CurrentColumn    string `json:"current_column,omitempty"`
	CompletedColumns int    `json:"completed_columns"`
	TotalColumns     int    `json:"total_columns"`
}

// ProgressReporter interface for reporting progress
type ProgressReporter interface {
	// Report reports the current progress
	Report(p Progress)
	// IsCancelled returns true if the operation has been cancelled
	IsCancelled() bool
	// Cancel cancels the operation
	Cancel()
}

// CLIProgressReporter implements ProgressReporter for CLI mode
type CLIProgressReporter struct {
	writer    io.Writer
	cancelled atomic.Bool
	lastLine  string
	mu        sync.Mutex
}

// NewCLIProgressReporter creates a new CLI progress reporter
func NewCLIProgressReporter(w io.Writer) *CLIProgressReporter {
	return &CLIProgressReporter{
		writer: w,
	}
}

// Report implements ProgressReporter
func (r *CLIProgressReporter) Report(p Progress) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var line string
	switch p.Stage {
	case StageAnalyzing:
		line = "[1/4] Analyzing schema..."
	case StageCollectingStats:
		if p.CurrentTable != "" && p.CurrentColumn != "" {
			line = fmt.Sprintf("[2/4] Collecting stats... %s.%s (%d/%d columns)",
				p.CurrentTable, p.CurrentColumn, p.CompletedColumns, p.TotalColumns)
		} else {
			line = "[2/4] Collecting stats..."
		}
	case StageInferring:
		line = "[3/4] Running inference..."
	case StageCompleted:
		line = "[4/4] Completed"
	case StageFailed:
		line = "Failed"
	case StageCancelled:
		line = "Cancelled (checkpoint saved)"
	}

	// Clear previous line and print new one
	if r.lastLine != "" {
		fmt.Fprintf(r.writer, "\r%*s\r", len(r.lastLine), "")
	}
	fmt.Fprint(r.writer, line)

	// Add newline for final states
	if p.Stage == StageCompleted || p.Stage == StageFailed || p.Stage == StageCancelled {
		fmt.Fprintln(r.writer)
	}

	r.lastLine = line
}

// IsCancelled implements ProgressReporter
func (r *CLIProgressReporter) IsCancelled() bool {
	return r.cancelled.Load()
}

// Cancel implements ProgressReporter
func (r *CLIProgressReporter) Cancel() {
	r.cancelled.Store(true)
}

// TaskStatus represents the status of an async task
type TaskStatus struct {
	TaskID               string    `json:"task_id"`
	Status               string    `json:"status"` // pending, running, completed, failed, cancelled
	Stage                Stage     `json:"stage,omitempty"`
	Progress             *Progress `json:"progress,omitempty"`
	ResumedFromCheckpoint bool     `json:"resumed_from_checkpoint,omitempty"`
	StartedAt            time.Time `json:"started_at,omitempty"`
	CompletedAt          time.Time `json:"completed_at,omitempty"`
	Error                string    `json:"error,omitempty"`
	Result               any       `json:"result,omitempty"`
}

// TaskStore stores task status for async operations
type TaskStore struct {
	tasks map[string]*TaskStatus
	mu    sync.RWMutex
}

// NewTaskStore creates a new task store
func NewTaskStore() *TaskStore {
	return &TaskStore{
		tasks: make(map[string]*TaskStatus),
	}
}

// Get returns a task status by ID
func (s *TaskStore) Get(taskID string) (*TaskStatus, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	task, ok := s.tasks[taskID]
	if !ok {
		return nil, false
	}
	// Return a copy to avoid race conditions
	taskCopy := *task
	if task.Progress != nil {
		progressCopy := *task.Progress
		taskCopy.Progress = &progressCopy
	}
	return &taskCopy, true
}

// Set stores a task status
func (s *TaskStore) Set(taskID string, status *TaskStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[taskID] = status
}

// Update updates a task status
func (s *TaskStore) Update(taskID string, fn func(*TaskStatus)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if task, ok := s.tasks[taskID]; ok {
		fn(task)
	}
}

// Delete removes a task status
func (s *TaskStore) Delete(taskID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tasks, taskID)
}

// TaskProgressReporter implements ProgressReporter for REST API mode
type TaskProgressReporter struct {
	taskID string
	store  *TaskStore
}

// NewTaskProgressReporter creates a new task progress reporter
func NewTaskProgressReporter(taskID string, store *TaskStore) *TaskProgressReporter {
	return &TaskProgressReporter{
		taskID: taskID,
		store:  store,
	}
}

// Report implements ProgressReporter
func (r *TaskProgressReporter) Report(p Progress) {
	r.store.Update(r.taskID, func(task *TaskStatus) {
		task.Stage = p.Stage
		task.Progress = &p

		switch p.Stage {
		case StageCompleted:
			task.Status = "completed"
			task.CompletedAt = time.Now()
		case StageFailed:
			task.Status = "failed"
			task.CompletedAt = time.Now()
		case StageCancelled:
			task.Status = "cancelled"
			task.CompletedAt = time.Now()
		default:
			// Only update to running if not already cancelled
			if task.Status != "cancelled" {
				task.Status = "running"
			}
		}
	})
}

// IsCancelled implements ProgressReporter
// Checks the task store to see if the task has been cancelled
func (r *TaskProgressReporter) IsCancelled() bool {
	task, ok := r.store.Get(r.taskID)
	if !ok {
		return false
	}
	return task.Status == "cancelled"
}

// Cancel implements ProgressReporter
func (r *TaskProgressReporter) Cancel() {
	r.store.Update(r.taskID, func(task *TaskStatus) {
		task.Status = "cancelled"
		task.Stage = StageCancelled
		task.CompletedAt = time.Now()
	})
}

// NopProgressReporter is a no-op progress reporter
type NopProgressReporter struct{}

// NewNopProgressReporter creates a new no-op progress reporter
func NewNopProgressReporter() *NopProgressReporter {
	return &NopProgressReporter{}
}

// Report implements ProgressReporter
func (r *NopProgressReporter) Report(p Progress) {}

// IsCancelled implements ProgressReporter
func (r *NopProgressReporter) IsCancelled() bool {
	return false
}

// Cancel implements ProgressReporter
func (r *NopProgressReporter) Cancel() {}
