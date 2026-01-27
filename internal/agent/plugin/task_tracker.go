// Package plugin provides task tracking and persistence for plugin tasks
package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/imkerbos/mxsec-platform/api/proto/grpc"
)

// TaskStatus represents the status of a task
type TaskStatus string

const (
	TaskStatusReceived   TaskStatus = "received"   // Task received from server
	TaskStatusDispatched TaskStatus = "dispatched" // Task dispatched to plugin
	TaskStatusCompleted  TaskStatus = "completed"  // Task completed by plugin
	TaskStatusFailed     TaskStatus = "failed"     // Task failed
)

// TrackedTask represents a task with tracking information
type TrackedTask struct {
	Task         *grpc.Task `json:"task"`
	Status       TaskStatus `json:"status"`
	PluginName   string     `json:"plugin_name"`
	ReceivedAt   time.Time  `json:"received_at"`
	DispatchedAt time.Time  `json:"dispatched_at,omitempty"`
	CompletedAt  time.Time  `json:"completed_at,omitempty"`
	RetryCount   int        `json:"retry_count"`
}

// TaskTracker tracks and persists plugin tasks
type TaskTracker struct {
	workDir string
	logger  *zap.Logger
	tasks   map[string]*TrackedTask // token -> TrackedTask
	mu      sync.RWMutex
}

// NewTaskTracker creates a new task tracker
func NewTaskTracker(workDir string, logger *zap.Logger) (*TaskTracker, error) {
	taskDir := filepath.Join(workDir, "tasks")
	if err := os.MkdirAll(taskDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create task directory: %w", err)
	}

	tracker := &TaskTracker{
		workDir: taskDir,
		logger:  logger,
		tasks:   make(map[string]*TrackedTask),
	}

	// Load existing tasks from disk
	if err := tracker.loadTasks(); err != nil {
		logger.Warn("failed to load existing tasks", zap.Error(err))
	}

	return tracker, nil
}

// TrackTask tracks a new task
func (t *TaskTracker) TrackTask(task *grpc.Task, pluginName string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	tracked := &TrackedTask{
		Task:       task,
		Status:     TaskStatusReceived,
		PluginName: pluginName,
		ReceivedAt: time.Now(),
	}

	t.tasks[task.Token] = tracked

	// Persist to disk
	if err := t.saveTask(tracked); err != nil {
		return fmt.Errorf("failed to save task: %w", err)
	}

	t.logger.Info("task tracked",
		zap.String("token", task.Token),
		zap.String("plugin", pluginName),
		zap.String("status", string(TaskStatusReceived)))

	return nil
}

// MarkDispatched marks a task as dispatched to plugin
func (t *TaskTracker) MarkDispatched(token string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	tracked, ok := t.tasks[token]
	if !ok {
		return fmt.Errorf("task not found: %s", token)
	}

	tracked.Status = TaskStatusDispatched
	tracked.DispatchedAt = time.Now()

	// Persist to disk
	if err := t.saveTask(tracked); err != nil {
		return fmt.Errorf("failed to save task: %w", err)
	}

	t.logger.Debug("task marked as dispatched", zap.String("token", token))
	return nil
}

// MarkCompleted marks a task as completed and removes it from tracking
func (t *TaskTracker) MarkCompleted(token string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	tracked, ok := t.tasks[token]
	if !ok {
		return fmt.Errorf("task not found: %s", token)
	}

	tracked.Status = TaskStatusCompleted
	tracked.CompletedAt = time.Now()

	// Remove from memory
	delete(t.tasks, token)

	// Remove from disk
	if err := t.removeTask(token); err != nil {
		t.logger.Warn("failed to remove completed task file", zap.String("token", token), zap.Error(err))
	}

	t.logger.Info("task marked as completed",
		zap.String("token", token),
		zap.Duration("duration", tracked.CompletedAt.Sub(tracked.ReceivedAt)))

	return nil
}

// MarkFailed marks a task as failed
func (t *TaskTracker) MarkFailed(token string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	tracked, ok := t.tasks[token]
	if !ok {
		return fmt.Errorf("task not found: %s", token)
	}

	tracked.Status = TaskStatusFailed
	tracked.CompletedAt = time.Now()

	// Remove from memory
	delete(t.tasks, token)

	// Remove from disk
	if err := t.removeTask(token); err != nil {
		t.logger.Warn("failed to remove failed task file", zap.String("token", token), zap.Error(err))
	}

	t.logger.Info("task marked as failed", zap.String("token", token))
	return nil
}

// GetPendingTasks returns tasks that need to be retried (received or dispatched but not completed)
func (t *TaskTracker) GetPendingTasks(pluginName string) []*grpc.Task {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var pending []*grpc.Task
	for _, tracked := range t.tasks {
		if tracked.PluginName == pluginName &&
			(tracked.Status == TaskStatusReceived || tracked.Status == TaskStatusDispatched) {
			pending = append(pending, tracked.Task)
			t.logger.Info("found pending task for retry",
				zap.String("token", tracked.Task.Token),
				zap.String("plugin", pluginName),
				zap.String("status", string(tracked.Status)),
				zap.Time("received_at", tracked.ReceivedAt))
		}
	}

	return pending
}

// saveTask saves a task to disk
func (t *TaskTracker) saveTask(tracked *TrackedTask) error {
	data, err := json.Marshal(tracked)
	if err != nil {
		return fmt.Errorf("failed to marshal task: %w", err)
	}

	filePath := filepath.Join(t.workDir, tracked.Task.Token+".json")
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write task file: %w", err)
	}

	return nil
}

// removeTask removes a task file from disk
func (t *TaskTracker) removeTask(token string) error {
	filePath := filepath.Join(t.workDir, token+".json")
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove task file: %w", err)
	}
	return nil
}

// loadTasks loads existing tasks from disk
func (t *TaskTracker) loadTasks() error {
	files, err := os.ReadDir(t.workDir)
	if err != nil {
		return fmt.Errorf("failed to read task directory: %w", err)
	}

	loadedCount := 0
	for _, file := range files {
		if file.IsDir() || filepath.Ext(file.Name()) != ".json" {
			continue
		}

		filePath := filepath.Join(t.workDir, file.Name())
		data, err := os.ReadFile(filePath)
		if err != nil {
			t.logger.Warn("failed to read task file", zap.String("file", file.Name()), zap.Error(err))
			continue
		}

		var tracked TrackedTask
		if err := json.Unmarshal(data, &tracked); err != nil {
			t.logger.Warn("failed to unmarshal task", zap.String("file", file.Name()), zap.Error(err))
			continue
		}

		// Only load tasks that are not completed
		if tracked.Status != TaskStatusCompleted && tracked.Status != TaskStatusFailed {
			t.tasks[tracked.Task.Token] = &tracked
			loadedCount++
			t.logger.Info("loaded pending task from disk",
				zap.String("token", tracked.Task.Token),
				zap.String("plugin", tracked.PluginName),
				zap.String("status", string(tracked.Status)))
		} else {
			// Clean up completed/failed task files
			os.Remove(filePath)
		}
	}

	if loadedCount > 0 {
		t.logger.Info("loaded pending tasks from disk", zap.Int("count", loadedCount))
	}

	return nil
}

// CleanupOldTasks removes tasks older than the specified duration
func (t *TaskTracker) CleanupOldTasks(maxAge time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	for token, tracked := range t.tasks {
		if now.Sub(tracked.ReceivedAt) > maxAge {
			t.logger.Warn("removing stale task",
				zap.String("token", token),
				zap.Duration("age", now.Sub(tracked.ReceivedAt)))
			delete(t.tasks, token)
			t.removeTask(token)
		}
	}
}
