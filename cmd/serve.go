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
	"github.com/k1LoW/tbls/schema"
	"github.com/k1LoW/tbls/stats"
	"github.com/spf13/cobra"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	_ "github.com/k1LoW/tbls/docs" // swagger docs
)

var (
	serveAddr string
)

// taskStore holds all task states
var taskStore = stats.NewTaskStore()

// @title tbls serve API
// @version 1.0
// @description REST API for tbls database schema analysis.
// @description
// @description `tbls serve` starts an HTTP server that provides endpoints to analyze databases
// @description and return schema information as JSON. Supports asynchronous task execution with
// @description progress polling, checkpoint/resume, and task cancellation.

// @license.name MIT
// @license.url https://github.com/k1LoW/tbls/blob/main/LICENSE

// @host localhost:8080
// @BasePath /

// @tag.name Schema
// @tag.description Database schema analysis operations

// serveCmd represents the serve command.
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "start a server to serve schema API",
	Long:  `'tbls serve' starts an HTTP server that provides a /schema endpoint to analyze databases.`,
	RunE: func(_ *cobra.Command, _ []string) error {
		gin.SetMode(gin.ReleaseMode)
		r := gin.Default()

		// Swagger UI
		r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

		// Async schema analysis
		r.POST("/schema", handleSchemaAsync)

		// Get task status
		r.GET("/schema/status/:task_id", handleSchemaStatus)

		// Cancel task
		r.DELETE("/schema/:task_id", handleSchemaCancel)

		return r.Run(serveAddr)
	},
}

// handleSchemaAsync godoc
// @Summary Submit schema analysis task
// @Description Analyze a database asynchronously. Returns a task ID immediately for progress polling.
// @Tags Schema
// @Accept json
// @Produce json
// @Param request body SchemaRequest true "Schema analysis request"
// @Success 202 {object} TaskAcceptedResponse "Task accepted"
// @Failure 400 {object} ErrorResponse "Bad request"
// @Router /schema [post]
func handleSchemaAsync(c *gin.Context) {
	var req SchemaRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: err.Error(),
		})
		return
	}

	// Validate DSN is required
	if req.DSN.URL == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: "dsn.url is required",
		})
		return
	}

	// Convert to config
	cfg := req.toConfig()

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
	go processSchemaAsync(taskID, cfg)

	c.JSON(http.StatusAccepted, TaskAcceptedResponse{
		TaskID: taskID,
		Status: "pending",
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

// handleSchemaStatus godoc
// @Summary Get task status
// @Description Get the status and progress of a schema analysis task.
// @Tags Schema
// @Produce json
// @Param task_id path string true "Task ID" format(uuid)
// @Success 200 {object} TaskStatusResponse "Task status"
// @Failure 404 {object} ErrorResponse "Task not found"
// @Router /schema/status/{task_id} [get]
func handleSchemaStatus(c *gin.Context) {
	taskID := c.Param("task_id")

	task, ok := taskStore.Get(taskID)
	if !ok {
		c.JSON(http.StatusNotFound, ErrorResponse{
			Error: "task not found",
		})
		return
	}

	response := TaskStatusResponse{
		TaskID: task.TaskID,
		Status: task.Status,
		Stage:  task.Stage,
	}

	if task.Progress != nil {
		response.Progress = &ProgressInfo{
			CurrentTable:     task.Progress.CurrentTable,
			CurrentColumn:    task.Progress.CurrentColumn,
			CompletedColumns: task.Progress.CompletedColumns,
			TotalColumns:     task.Progress.TotalColumns,
		}
	}

	response.ResumedFromCheckpoint = task.ResumedFromCheckpoint

	if !task.StartedAt.IsZero() {
		response.StartedAt = task.StartedAt
	}

	if !task.CompletedAt.IsZero() {
		response.CompletedAt = task.CompletedAt
	}

	if task.Error != "" {
		response.Error = task.Error
	}

	if task.Result != nil && task.Status == "completed" {
		if result, ok := task.Result.(*schema.Schema); ok {
			response.Result = result
		}
	}

	c.JSON(http.StatusOK, response)
}

// handleSchemaCancel godoc
// @Summary Cancel task
// @Description Cancel a running task. The current progress is saved to a checkpoint file.
// @Tags Schema
// @Produce json
// @Param task_id path string true "Task ID" format(uuid)
// @Success 200 {object} TaskCancelledResponse "Task cancelled"
// @Failure 400 {object} TaskNotRunningError "Task is not running"
// @Failure 404 {object} ErrorResponse "Task not found"
// @Router /schema/{task_id} [delete]
func handleSchemaCancel(c *gin.Context) {
	taskID := c.Param("task_id")

	task, ok := taskStore.Get(taskID)
	if !ok {
		c.JSON(http.StatusNotFound, ErrorResponse{
			Error: "task not found",
		})
		return
	}

	// Check if task is still running
	if task.Status != "running" && task.Status != "pending" {
		c.JSON(http.StatusBadRequest, TaskNotRunningError{
			Error:  "task is not running",
			Status: task.Status,
			TaskID: taskID,
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

	c.JSON(http.StatusOK, TaskCancelledResponse{
		TaskID:          taskID,
		Status:          "cancelled",
		CheckpointSaved: true,
	})
}

func init() {
	rootCmd.AddCommand(serveCmd)
	serveCmd.Flags().StringVarP(&serveAddr, "addr", "a", ":8080", "server listen address")
}
