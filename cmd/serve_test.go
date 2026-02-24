//go:build postgres && clickhouse

package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/k1LoW/tbls/schema"
	"github.com/k1LoW/tbls/stats"
)

const testOutputDir = "testdata/serve"

func init() {
	gin.SetMode(gin.TestMode)
}

// setupTestRouter creates a Gin router with all serve handlers registered.
func setupTestRouter() *gin.Engine {
	r := gin.New()
	r.POST("/scaffold", handleScaffold)
	r.POST("/schema", handleSchemaAsync)
	r.POST("/schema_sync", handleSchemaSync)
	r.GET("/schema/status/:task_id", handleSchemaStatus)
	r.DELETE("/schema/:task_id", handleSchemaCancel)
	return r
}

// resetTaskStore resets the global taskStore between async tests.
func resetTaskStore() {
	taskStore = stats.NewTaskStore()
}

// postJSON sends a POST request with JSON body and returns the response.
func postJSON(t *testing.T, router *gin.Engine, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// getJSON sends a GET request and returns the response.
func getJSON(t *testing.T, router *gin.Engine, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// deleteJSON sends a DELETE request and returns the response.
func deleteJSON(t *testing.T, router *gin.Engine, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, path, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// taskStatusRaw is a test-local struct for deserializing TaskStatusResponse
// where Result is json.RawMessage (since the internal TaskStatus.Result is `any`).
type taskStatusRaw struct {
	TaskID              string           `json:"task_id"`
	Status              string           `json:"status"`
	Stage               string           `json:"stage,omitempty"`
	StartedAt           time.Time        `json:"started_at,omitempty"`
	CompletedAt         time.Time        `json:"completed_at,omitempty"`
	Error               string           `json:"error,omitempty"`
	ResumedFromCheckpoint bool           `json:"resumed_from_checkpoint,omitempty"`
	Result              json.RawMessage  `json:"result,omitempty"`
}

// pollTaskStatus polls GET /schema/status/:id until a terminal state is reached.
// Returns the final status response. Fails the test on timeout.
func pollTaskStatus(t *testing.T, router *gin.Engine, taskID string) taskStatusRaw {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatalf("pollTaskStatus: timeout waiting for task %s to complete", taskID)
		}
		w := getJSON(t, router, "/schema/status/"+taskID)
		if w.Code != http.StatusOK {
			t.Fatalf("pollTaskStatus: unexpected status %d: %s", w.Code, w.Body.String())
		}
		var raw taskStatusRaw
		if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
			t.Fatalf("pollTaskStatus: unmarshal: %v", err)
		}
		switch raw.Status {
		case "completed", "failed", "cancelled":
			return raw
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// postgresDSN returns the test PostgreSQL DSN or skips the test.
func postgresDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("TBLS_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TBLS_TEST_POSTGRES_DSN not set")
	}
	return dsn
}

// clickhouseDSN returns the test ClickHouse DSN or skips the test.
func clickhouseDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("TBLS_TEST_CLICKHOUSE_DSN")
	if dsn == "" {
		t.Skip("TBLS_TEST_CLICKHOUSE_DSN not set")
	}
	return dsn
}

// findTable finds a table by name in the schema.
func findTable(s *schema.Schema, name string) *schema.Table {
	for _, t := range s.Tables {
		if t.Name == name {
			return t
		}
	}
	return nil
}

// saveTestOutput writes data as indented JSON to testdata/serve/<filename>.
func saveTestOutput(t *testing.T, filename string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(testOutputDir, 0o755); err != nil {
		t.Logf("warning: failed to create output dir: %v", err)
		return
	}
	// Re-indent for readability
	var buf bytes.Buffer
	if err := json.Indent(&buf, data, "", "  "); err != nil {
		// Fall back to raw bytes if indent fails
		buf.Reset()
		buf.Write(data)
	}
	p := filepath.Join(testOutputDir, filename)
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Logf("warning: failed to write %s: %v", p, err)
		return
	}
	t.Logf("saved output to %s", p)
}

func TestServeAPI(t *testing.T) {
	router := setupTestRouter()

	// ── Scaffold ──────────────────────────────────────────────────

	t.Run("Scaffold/Postgres", func(t *testing.T) {
		dsn := postgresDSN(t)
		w := postJSON(t, router, "/scaffold", ScaffoldRequest{
			DSN: DSNConfig{URL: dsn},
		})
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		saveTestOutput(t, "scaffold_postgres.json", w.Body.Bytes())
		var resp ScaffoldResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp.Config == nil {
			t.Fatal("config is nil")
		}
		if len(resp.Config.Stats.Tables) == 0 {
			t.Fatal("expected scaffold config to contain tables with stats settings")
		}
	})

	t.Run("Scaffold/ClickHouse", func(t *testing.T) {
		dsn := clickhouseDSN(t)
		w := postJSON(t, router, "/scaffold", ScaffoldRequest{
			DSN: DSNConfig{URL: dsn},
		})
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		saveTestOutput(t, "scaffold_clickhouse.json", w.Body.Bytes())
		var resp ScaffoldResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp.Config == nil {
			t.Fatal("config is nil")
		}
		if len(resp.Config.Stats.Tables) == 0 {
			t.Fatal("expected scaffold config to contain tables")
		}
	})

	t.Run("Scaffold/EmptyDSN", func(t *testing.T) {
		w := postJSON(t, router, "/scaffold", ScaffoldRequest{
			DSN: DSNConfig{URL: ""},
		})
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
		}
	})

	// ── SchemaSync ────────────────────────────────────────────────

	t.Run("SchemaSync/Postgres", func(t *testing.T) {
		dsn := postgresDSN(t)
		w := postJSON(t, router, "/schema_sync", SchemaRequest{
			DSN: DSNConfig{URL: dsn},
		})
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		saveTestOutput(t, "schema_sync_postgres.json", w.Body.Bytes())
		var resp SchemaSyncResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp.Schema == nil {
			t.Fatal("schema is nil")
		}
		if len(resp.Schema.Tables) == 0 {
			t.Fatal("expected schema to have tables")
		}
		tbl := findTable(resp.Schema, "public.users")
		if tbl == nil {
			t.Fatal("expected table 'public.users' to exist")
		}
	})

	t.Run("SchemaSync/PostgresWithStats", func(t *testing.T) {
		dsn := postgresDSN(t)
		w := postJSON(t, router, "/schema_sync", SchemaRequest{
			DSN: DSNConfig{URL: dsn},
			Stats: &StatsConfig{
				Enabled:    true,
				Include:    []string{"public.stats_basic"},
				SampleSize: -1,
			},
			Force: true,
		})
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		saveTestOutput(t, "schema_sync_postgres_with_stats.json", w.Body.Bytes())
		var resp SchemaSyncResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp.Schema == nil {
			t.Fatal("schema is nil")
		}
		tbl := findTable(resp.Schema, "public.stats_basic")
		if tbl == nil {
			t.Fatal("expected table 'public.stats_basic' to exist")
		}
		if tbl.Stats == nil {
			t.Fatal("expected stats_basic to have table stats")
		}
		if tbl.Stats.RowCount != 10000 {
			t.Errorf("expected stats_basic.RowCount == 10000, got %d", tbl.Stats.RowCount)
		}
	})

	t.Run("SchemaSync/ClickHouse", func(t *testing.T) {
		dsn := clickhouseDSN(t)
		w := postJSON(t, router, "/schema_sync", SchemaRequest{
			DSN: DSNConfig{URL: dsn},
		})
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		saveTestOutput(t, "schema_sync_clickhouse.json", w.Body.Bytes())
		var resp SchemaSyncResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp.Schema == nil {
			t.Fatal("schema is nil")
		}
		if len(resp.Schema.Tables) == 0 {
			t.Fatal("expected schema to have tables")
		}
	})

	t.Run("SchemaSync/ClickHouseWithStats", func(t *testing.T) {
		dsn := clickhouseDSN(t)
		w := postJSON(t, router, "/schema_sync", SchemaRequest{
			DSN: DSNConfig{URL: dsn},
			Stats: &StatsConfig{
				Enabled:    true,
				Include:    []string{"stats_basic"},
				SampleSize: -1,
			},
			Force: true,
		})
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		saveTestOutput(t, "schema_sync_clickhouse_with_stats.json", w.Body.Bytes())
		var resp SchemaSyncResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp.Schema == nil {
			t.Fatal("schema is nil")
		}
		tbl := findTable(resp.Schema, "stats_basic")
		if tbl == nil {
			t.Fatal("expected table 'stats_basic' to exist")
		}
		if tbl.Stats == nil {
			t.Fatal("expected stats_basic to have table stats")
		}
		if tbl.Stats.RowCount != 10000 {
			t.Errorf("expected stats_basic.RowCount == 10000, got %d", tbl.Stats.RowCount)
		}
	})

	t.Run("SchemaSync/EmptyDSN", func(t *testing.T) {
		w := postJSON(t, router, "/schema_sync", SchemaRequest{
			DSN: DSNConfig{URL: ""},
		})
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
		}
	})

	// ── SchemaAsync ───────────────────────────────────────────────

	t.Run("SchemaAsync/Postgres", func(t *testing.T) {
		resetTaskStore()
		dsn := postgresDSN(t)
		w := postJSON(t, router, "/schema", SchemaRequest{
			DSN: DSNConfig{URL: dsn},
		})
		if w.Code != http.StatusAccepted {
			t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
		}
		var accepted TaskAcceptedResponse
		if err := json.Unmarshal(w.Body.Bytes(), &accepted); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if accepted.TaskID == "" {
			t.Fatal("expected non-empty task_id")
		}

		result := pollTaskStatus(t, router, accepted.TaskID)
		if result.Status != "completed" {
			t.Fatalf("expected status 'completed', got %q (error: %s)", result.Status, result.Error)
		}
		if result.Result == nil {
			t.Fatal("expected result to be non-nil")
		}
		saveTestOutput(t, "schema_async_postgres.json", result.Result)

		var s schema.Schema
		if err := json.Unmarshal(result.Result, &s); err != nil {
			t.Fatalf("unmarshal schema result: %v", err)
		}
		if len(s.Tables) == 0 {
			t.Fatal("expected schema to have tables")
		}
	})

	t.Run("SchemaAsync/ClickHouse", func(t *testing.T) {
		resetTaskStore()
		dsn := clickhouseDSN(t)
		w := postJSON(t, router, "/schema", SchemaRequest{
			DSN: DSNConfig{URL: dsn},
		})
		if w.Code != http.StatusAccepted {
			t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
		}
		var accepted TaskAcceptedResponse
		if err := json.Unmarshal(w.Body.Bytes(), &accepted); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if accepted.TaskID == "" {
			t.Fatal("expected non-empty task_id")
		}

		result := pollTaskStatus(t, router, accepted.TaskID)
		if result.Status != "completed" {
			t.Fatalf("expected status 'completed', got %q (error: %s)", result.Status, result.Error)
		}
		if result.Result == nil {
			t.Fatal("expected result to be non-nil")
		}
		saveTestOutput(t, "schema_async_clickhouse.json", result.Result)

		var s schema.Schema
		if err := json.Unmarshal(result.Result, &s); err != nil {
			t.Fatalf("unmarshal schema result: %v", err)
		}
		if len(s.Tables) == 0 {
			t.Fatal("expected schema to have tables")
		}
	})

	// ── Status / Cancel edge cases ────────────────────────────────

	t.Run("Status/NotFound", func(t *testing.T) {
		w := getJSON(t, router, "/schema/status/nonexistent-id")
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
		}
		var resp ErrorResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp.Error != "task not found" {
			t.Errorf("expected error 'task not found', got %q", resp.Error)
		}
	})

	t.Run("Cancel/NotFound", func(t *testing.T) {
		w := deleteJSON(t, router, "/schema/nonexistent-id")
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
		}
		var resp ErrorResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp.Error != "task not found" {
			t.Errorf("expected error 'task not found', got %q", resp.Error)
		}
	})
}
