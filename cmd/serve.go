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
	"github.com/k1LoW/tbls/checkpoint"
	"github.com/k1LoW/tbls/config"
	"github.com/k1LoW/tbls/datasource"
	"github.com/k1LoW/tbls/schema"
	"github.com/k1LoW/tbls/stats"
	"github.com/sirupsen/logrus"
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

		// CORS middleware - allow all origins
		r.Use(func(c *gin.Context) {
			c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
			c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			c.Writer.Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization")
			c.Writer.Header().Set("Access-Control-Max-Age", "86400")

			if c.Request.Method == "OPTIONS" {
				c.AbortWithStatus(http.StatusNoContent)
				return
			}

			c.Next()
		})

		// Swagger UI
		r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

		// Scaffold endpoint - generate complete config
		r.POST("/scaffold", handleScaffold)

		// Async schema analysis
		r.POST("/schema", handleSchemaAsync)

		// Synchronous schema analysis
		r.POST("/schema_sync", handleSchemaSync)

		// Get task status
		r.GET("/schema/status/:task_id", handleSchemaStatus)

		// Cancel task
		r.DELETE("/schema/:task_id", handleSchemaCancel)

		// Print startup message with clickable links
		baseURL := formatBaseURL(serveAddr)
		logrus.Infof("tbls serve starting...")
		logrus.Infof("  API:     %s", baseURL)
		logrus.Infof("  Swagger: %s/swagger/index.html", baseURL)

		return r.Run(serveAddr)
	},
}

// handleScaffold godoc
// @Summary Generate scaffolded config
// @Description Generate a complete configuration file with all parameters filled in from database schema.
// @Description This is useful for quick start - users can modify the generated config before running schema analysis.
// @Tags Schema
// @Accept json
// @Produce json
// @Param request body ScaffoldRequest true "Scaffold request with DSN"
// @Success 200 {object} ScaffoldResponse "Scaffolded configuration"
// @Failure 400 {object} ErrorResponse "Bad request"
// @Failure 500 {object} ErrorResponse "Internal server error"
// @Router /scaffold [post]
func handleScaffold(c *gin.Context) {
	var req ScaffoldRequest
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

	// Create config from request
	cfg := &config.Config{
		DSN: config.DSN{
			URL:     req.DSN.URL,
			Headers: req.DSN.Headers,
		},
	}

	// Analyze database schema (without stats)
	s, err := datasource.Analyze(cfg.DSN)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: err.Error(),
		})
		return
	}

	// Detect date columns for stats configuration
	detectedDateColumns, err := datasource.DetectDateColumns(cfg.DSN, s)
	if err != nil {
		// Log warning but continue - date column detection is optional
		logrus.WithError(err).Warn("failed to detect date columns")
		detectedDateColumns = make(map[string]string)
	}

	// Generate scaffolded config using existing logic
	scaffolded, err := GenerateScaffoldConfig(cfg, s, detectedDateColumns)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: err.Error(),
		})
		return
	}

	// Convert to API response format
	apiConfig := convertToAPIScaffoldConfig(scaffolded)

	c.JSON(http.StatusOK, ScaffoldResponse{
		Config: apiConfig,
	})
}

// convertToAPIScaffoldConfig converts internal ScaffoldConfig to API response format
func convertToAPIScaffoldConfig(s *ScaffoldConfig) *APIScaffoldConfig {
	// Convert tables list
	var tables []APIScaffoldTableStatConfig
	for _, v := range s.Stats.Tables {
		tables = append(tables, APIScaffoldTableStatConfig{
			Name:       v.Name,
			Mode:       string(v.Mode),
			DateColumn: v.DateColumn,
			SampleSize: v.SampleSize,
		})
	}

	return &APIScaffoldConfig{
		Name:    s.Name,
		Desc:    s.Desc,
		Labels:  s.Labels,
		DSN:     s.DSN,
		Include: s.Include,
		Exclude: s.Exclude,
		Sort:    s.Sort,
		DetectVirtualRelations: APIScaffoldDetectVirtualRelConfig{
			Enabled:  s.DetectVirtualRelations.Enabled,
			Strategy: s.DetectVirtualRelations.Strategy,
		},
		Stats: APIScaffoldStatsConfig{
			Enabled:             s.Stats.Enabled,
			Include:             s.Stats.Include,
			Exclude:             s.Stats.Exclude,
			TopN:                s.Stats.TopN,
			SampleSize:          s.Stats.SampleSize,
			LargeTableThreshold: s.Stats.LargeTableThreshold,
			RecentDays:          s.Stats.RecentDays,
			Inference: APIScaffoldInferenceConfig{
				Enabled:                 s.Stats.Inference.Enabled,
				EnumMaxCardinality:      s.Stats.Inference.EnumMaxCardinality,
				EnumMaxDistinct:         s.Stats.Inference.EnumMaxDistinct,
				DictMaxCardinality:      s.Stats.Inference.DictMaxCardinality,
				DictMaxDistinct:         s.Stats.Inference.DictMaxDistinct,
				ForeignKeyMinConfidence: s.Stats.Inference.ForeignKeyMinConfidence,
			},
			Checkpoint: APIScaffoldCheckpointConfig{
				Enabled: s.Stats.Checkpoint.Enabled,
				TTL:     s.Stats.Checkpoint.TTL,
				Force:   s.Stats.Checkpoint.Force,
			},
			Tables: tables,
		},
		RequiredVersion: s.RequiredVersion,
	}
}

// handleSchemaAsync godoc
// @Summary Submit schema analysis task
// @Description Analyze a database asynchronously. Returns a task ID immediately for progress polling.
// @Description If a task is already running for the same DSN, returns the existing task ID.
// @Description Use force=true to ignore checkpoint and start fresh.
// @Description Use debug=true to include query logs in the response (useful for debugging stats collection).
// @Tags Schema
// @Accept json
// @Produce json
// @Param request body SchemaRequest true "Schema analysis request"
// @Success 202 {object} TaskAcceptedResponse "Task accepted"
// @Success 200 {object} TaskExistsResponse "Task already running for this DSN"
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

	// Calculate DSN hash for checkpoint addressing
	dsnHash := checkpoint.HashDSNURL(req.DSN.URL)

	// Check if there's already a running task for this DSN
	existingTaskID := taskStore.GetRunningByDSNHash(dsnHash)
	if existingTaskID != "" {
		c.JSON(http.StatusOK, TaskExistsResponse{
			TaskID:  existingTaskID,
			Status:  "running",
			Message: "task already running for this DSN",
		})
		return
	}

	// Convert to config
	cfg := req.toConfig()

	// Handle force flag - delete existing checkpoint
	if req.Force {
		checkpoint.DeleteCheckpoint(req.DSN.URL)
		cfg.Stats.Checkpoint.Force = true
	}

	// Generate task ID
	taskID := uuid.New().String()

	// Create task status
	taskStatus := &stats.TaskStatus{
		TaskID:    taskID,
		DSNHash:   dsnHash,
		Status:    "pending",
		Stage:     stats.StageAnalyzing,
		StartedAt: time.Now(),
	}
	taskStore.Set(taskID, taskStatus)

	// Track DSN hash to task ID mapping
	taskStore.SetDSNHash(dsnHash, taskID)

	// Start async processing
	go processSchemaAsync(taskID, dsnHash, cfg, req.Debug)

	c.JSON(http.StatusAccepted, TaskAcceptedResponse{
		TaskID: taskID,
		Status: "pending",
	})
}

// handleSchemaSync godoc
// @Summary Analyze schema synchronously
// @Description Analyze a database synchronously and return the schema directly.
// @Description This endpoint blocks until the analysis is complete.
// @Description Use this for small databases or when you don't need progress tracking.
// @Description Use debug=true to include query logs in the response (useful for debugging stats collection).
// @Tags Schema
// @Accept json
// @Produce json
// @Param request body SchemaRequest true "Schema analysis request"
// @Success 200 {object} SchemaSyncResponse "Schema analysis result"
// @Failure 400 {object} ErrorResponse "Bad request"
// @Failure 500 {object} ErrorResponse "Internal server error"
// @Router /schema_sync [post]
func handleSchemaSync(c *gin.Context) {
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

	// Handle force flag - delete existing checkpoint
	if req.Force {
		checkpoint.DeleteCheckpoint(req.DSN.URL)
		cfg.Stats.Checkpoint.Force = true
	}

	// Analyze database with stats if enabled
	var s *schema.Schema
	var err error
	if cfg.Stats.Enabled {
		s, err = datasource.AnalyzeWithStats(cfg.DSN, &cfg)
	} else {
		s, err = datasource.Analyze(cfg.DSN)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: err.Error(),
		})
		return
	}

	// Apply config modifications
	if err := cfg.ModifySchema(s); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: err.Error(),
		})
		return
	}

	// Clear queries if debug mode is not enabled
	if !req.Debug {
		clearQueriesFromSchema(s)
	}

	c.JSON(http.StatusOK, SchemaSyncResponse{
		Schema: s,
	})
}

func processSchemaAsync(taskID, dsnHash string, cfg config.Config, debug bool) {
	// Ensure DSN hash mapping is cleared when task completes
	defer taskStore.ClearDSNHash(dsnHash)

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

	// Clear queries if debug mode is not enabled
	if !debug {
		clearQueriesFromSchema(s)
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

// clearQueriesFromSchema removes query logs from schema statistics
// This is used when debug mode is disabled
func clearQueriesFromSchema(s *schema.Schema) {
	if s == nil {
		return
	}
	for _, table := range s.Tables {
		// Clear table-level queries
		if table.Stats != nil {
			table.Stats.Queries = nil
		}
		// Clear column-level queries
		for _, column := range table.Columns {
			if column.Stats != nil {
				column.Stats.Queries = nil
			}
		}
	}
}

// formatBaseURL converts a listen address to a full URL for display
func formatBaseURL(addr string) string {
	// Handle cases like ":8080" -> "http://localhost:8080"
	if len(addr) > 0 && addr[0] == ':' {
		return "http://localhost" + addr
	}
	// Handle cases like "0.0.0.0:8080" -> "http://localhost:8080"
	if len(addr) > 7 && addr[:7] == "0.0.0.0" {
		return "http://localhost" + addr[7:]
	}
	// Otherwise assume it's a host:port and add http://
	return "http://" + addr
}

func init() {
	rootCmd.AddCommand(serveCmd)
	serveCmd.Flags().StringVarP(&serveAddr, "addr", "a", ":8080", "server listen address")
}
