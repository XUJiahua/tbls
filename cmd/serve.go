// Copyright © 2018 Ken'ichiro Oyama <k1lowxb@gmail.com>
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package cmd

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/k1LoW/tbls/config"
	"github.com/k1LoW/tbls/datasource"
	"github.com/k1LoW/tbls/stats"
	"github.com/spf13/cobra"
)

var (
	serveAddr string
)

// taskStore holds all task states
var taskStore = stats.NewTaskStore()

// serveCmd represents the serve command.
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "start a server to serve schema API",
	Long:  `'tbls serve' starts an HTTP server that provides a /schema endpoint to analyze databases.`,
	RunE: func(_ *cobra.Command, _ []string) error {
		gin.SetMode(gin.ReleaseMode)
		r := gin.Default()

		// Async schema analysis
		r.POST("/schema", handleSchemaAsync)

		// Get task status
		r.GET("/schema/status/:task_id", handleSchemaStatus)

		// Cancel task
		r.DELETE("/schema/:task_id", handleSchemaCancel)

		return r.Run(serveAddr)
	},
}

// schemaRequest is the request body for /schema endpoint
type schemaRequest struct {
	config.Config
	Force bool `json:"force,omitempty"` // Force stats collection, ignoring checkpoint
}

func handleSchemaAsync(c *gin.Context) {
	var req schemaRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	// Validate DSN is required
	if req.DSN.URL == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "dsn.url is required",
		})
		return
	}

	// Apply force flag
	if req.Force {
		req.Stats.Checkpoint.Force = true
	}

	// Generate task ID
	taskID := uuid.New().String()

	// Create task status
	taskStatus := &stats.TaskStatus{
		TaskID:    taskID,
		Status:    "pending",
		Stage:     stats.StageAnalyzing,
		StartedAt: time.Now(),
	}
	taskStore.Set(taskID, taskStatus)

	// Start async processing
	go processSchemaAsync(taskID, req.Config)

	c.JSON(http.StatusAccepted, gin.H{
		"task_id": taskID,
		"status":  "pending",
	})
}

func processSchemaAsync(taskID string, cfg config.Config) {
	// Create progress reporter
	reporter := stats.NewTaskProgressReporter(taskID, taskStore)

	// Update status to running
	taskStore.Update(taskID, func(task *stats.TaskStatus) {
		task.Status = "running"
	})

	// Analyze database with progress reporting
	s, err := datasource.AnalyzeWithStatsAndProgress(cfg.DSN, &cfg, reporter)
	if err != nil {
		taskStore.Update(taskID, func(task *stats.TaskStatus) {
			task.Status = "failed"
			task.Error = err.Error()
			task.CompletedAt = time.Now()
		})
		return
	}

	// Apply config modifications
	if err := cfg.ModifySchema(s); err != nil {
		taskStore.Update(taskID, func(task *stats.TaskStatus) {
			task.Status = "failed"
			task.Error = err.Error()
			task.CompletedAt = time.Now()
		})
		return
	}

	// Store result
	taskStore.Update(taskID, func(task *stats.TaskStatus) {
		task.Status = "completed"
		task.Stage = stats.StageCompleted
		task.Result = s
		task.CompletedAt = time.Now()
	})
}

func handleSchemaStatus(c *gin.Context) {
	taskID := c.Param("task_id")

	task, ok := taskStore.Get(taskID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "task not found",
		})
		return
	}

	response := gin.H{
		"task_id": task.TaskID,
		"status":  task.Status,
		"stage":   task.Stage,
	}

	if task.Progress != nil {
		response["progress"] = gin.H{
			"current_table":     task.Progress.CurrentTable,
			"current_column":    task.Progress.CurrentColumn,
			"completed_columns": task.Progress.CompletedColumns,
			"total_columns":     task.Progress.TotalColumns,
		}
	}

	if task.ResumedFromCheckpoint {
		response["resumed_from_checkpoint"] = true
	}

	if !task.StartedAt.IsZero() {
		response["started_at"] = task.StartedAt
	}

	if !task.CompletedAt.IsZero() {
		response["completed_at"] = task.CompletedAt
	}

	if task.Error != "" {
		response["error"] = task.Error
	}

	if task.Result != nil && task.Status == "completed" {
		response["result"] = task.Result
	}

	c.JSON(http.StatusOK, response)
}

func handleSchemaCancel(c *gin.Context) {
	taskID := c.Param("task_id")

	task, ok := taskStore.Get(taskID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "task not found",
		})
		return
	}

	// Check if task is still running
	if task.Status != "running" && task.Status != "pending" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "task is not running",
			"status":  task.Status,
			"task_id": taskID,
		})
		return
	}

	// Get the reporter and cancel it
	// Note: The reporter is created inside processSchemaAsync, so we need
	// to update the task status directly. The reporter will check IsCancelled
	// during processing.
	taskStore.Update(taskID, func(t *stats.TaskStatus) {
		t.Status = "cancelled"
		t.Stage = stats.StageCancelled
		t.CompletedAt = time.Now()
	})

	c.JSON(http.StatusOK, gin.H{
		"task_id":          taskID,
		"status":           "cancelled",
		"checkpoint_saved": true,
	})
}

func init() {
	rootCmd.AddCommand(serveCmd)
	serveCmd.Flags().StringVarP(&serveAddr, "addr", "a", ":8080", "server listen address")
}
