package datasource

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/cli/safeexec"
	"github.com/k1LoW/errors"
	"github.com/k1LoW/ghfs"
	"github.com/k1LoW/go-github-client/v67/factory"
	"github.com/k1LoW/tbls/config"
	"github.com/k1LoW/tbls/drivers"
	"github.com/k1LoW/tbls/drivers/clickhouse"
	"github.com/k1LoW/tbls/drivers/mariadb"
	"github.com/k1LoW/tbls/drivers/mssql"
	"github.com/k1LoW/tbls/drivers/mysql"
	"github.com/k1LoW/tbls/drivers/postgres"
	"github.com/k1LoW/tbls/drivers/redshift"
	"github.com/k1LoW/tbls/drivers/snowflake"
	"github.com/k1LoW/tbls/drivers/sqlite"
	"github.com/k1LoW/tbls/schema"
	"github.com/k1LoW/tbls/stats"
	"github.com/sirupsen/logrus"
	"github.com/xo/dburl"
)

var supportDriversWithDburl = []string{
	"postgres",
	"mysql",
	"sqlite3",
	"sqlserver",
	"snowflake",
	"clickhouse",
}

// Analyze database.
func Analyze(dsn config.DSN) (*schema.Schema, error) {
	return AnalyzeContext(context.Background(), dsn)
}

// AnalyzeContext analyzes database with context for cancellation support.
func AnalyzeContext(ctx context.Context, dsn config.DSN) (_ *schema.Schema, err error) {
	defer func() {
		err = errors.WithStack(err)
	}()
	urlstr := dsn.URL
	if strings.HasPrefix(urlstr, "https://") || strings.HasPrefix(urlstr, "http://") {
		return AnalyzeHTTPResource(dsn)
	}
	if strings.HasPrefix(urlstr, "github://") {
		return AnalyzeGitHubContent(dsn)
	}
	if strings.HasPrefix(urlstr, "json://") {
		return AnalyzeJSON(urlstr)
	}
	if strings.HasPrefix(urlstr, "bq://") || strings.HasPrefix(urlstr, "bigquery://") {
		return AnalyzeBigquery(urlstr)
	}
	if strings.HasPrefix(urlstr, "span://") || strings.HasPrefix(urlstr, "spanner://") {
		return AnalyzeSpanner(urlstr)
	}
	if strings.HasPrefix(urlstr, "dynamodb://") || strings.HasPrefix(urlstr, "dynamo://") {
		return AnalyzeDynamodb(urlstr)
	}
	if strings.HasPrefix(urlstr, "mongodb://") || strings.HasPrefix(urlstr, "mongo://") {
		return AnalyzeMongodb(urlstr)
	}
	if strings.HasPrefix(urlstr, "databricks://") {
		return AnalyzeDatabricks(urlstr)
	}
	// ClickHouse HTTP protocol support: clickhouse+http:// or clickhouse+https://
	if strings.HasPrefix(urlstr, "clickhouse+http://") || strings.HasPrefix(urlstr, "clickhouse+https://") {
		return AnalyzeClickHouseHTTPContext(ctx, urlstr)
	}
	s := &schema.Schema{}
	u, err := dburl.Parse(urlstr)
	if err != nil || !slices.Contains(supportDriversWithDburl, u.Driver) {
		// Try ext driver
		return AnalyzeWithExtDriver(urlstr)
	}
	if err != nil {
		return nil, err
	}
	splitted := strings.Split(u.Short(), "/")
	if len(splitted) < 2 {
		return s, fmt.Errorf("invalid DSN: parse %s -> %#v", urlstr, u)
	}

	opts := []drivers.Option{}
	switch u.Driver {
	case "mysql":
		values := u.Query()
		for k := range values {
			if k == "show_auto_increment" {
				opts = append(opts, mysql.ShowAutoIcrrement())
				values.Del(k)
			}
			if k == "hide_auto_increment" {
				opts = append(opts, mysql.HideAutoIcrrement())
				values.Del(k)
			}
		}
		u.RawQuery = values.Encode()
		urlstr = u.String()
	case "sqlserver":
		values := u.Query()
		dbname := strings.TrimPrefix(u.Path, "/")
		values.Add("database", dbname)
		u.RawQuery = values.Encode()
		urlstr = u.String()
	}

	db, err := dburl.Open(urlstr)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	defer func() {
		_ = db.Close()
	}()
	if err := db.PingContext(ctx); err != nil {
		return nil, errors.WithStack(err)
	}

	var driver drivers.Driver

	switch u.Driver {
	case "postgres":
		s.Name = splitted[1]
		if u.Scheme == "rs" || u.Scheme == "redshift" {
			driver = redshift.New(db)
		} else {
			driver = postgres.New(db)
		}
	case "mysql":
		s.Name = splitted[1]
		if u.Scheme == "maria" || u.Scheme == "mariadb" {
			driver, err = mariadb.New(db, opts...)
		} else {
			driver, err = mysql.New(db, opts...)
		}
		if err != nil {
			return nil, err
		}
	case "sqlite3":
		s.Name = splitted[len(splitted)-1]
		driver = sqlite.New(db)
	case "sqlserver":
		s.Name = splitted[1]
		driver = mssql.New(db)
	case "snowflake":
		s.Name = splitted[2]
		driver = snowflake.New(db)
	case "clickhouse":
		s.Name = splitted[1]
		driver = clickhouse.New(db)
	default:
		return s, fmt.Errorf("unsupported driver '%s'", u.Driver)
	}
	err = driver.Analyze(ctx, s)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// AnalyzeWithStats analyzes database and optionally collects statistics
func AnalyzeWithStats(dsn config.DSN, cfg *config.Config) (*schema.Schema, error) {
	return AnalyzeWithStatsAndProgress(dsn, cfg, nil)
}

// AnalyzeWithStatsAndProgress analyzes database with optional progress reporting and checkpoint support
func AnalyzeWithStatsAndProgress(dsn config.DSN, cfg *config.Config, reporter stats.ProgressReporter) (*schema.Schema, error) {
	return AnalyzeWithStatsAndProgressContext(context.Background(), dsn, cfg, reporter)
}

// AnalyzeWithStatsAndProgressContext analyzes database with context for cancellation support
func AnalyzeWithStatsAndProgressContext(ctx context.Context, dsn config.DSN, cfg *config.Config, reporter stats.ProgressReporter) (*schema.Schema, error) {
	// Report analyzing stage
	if reporter != nil {
		reporter.Report(stats.Progress{Stage: stats.StageAnalyzing})
	}

	s, err := AnalyzeContext(ctx, dsn)
	if err != nil {
		if reporter != nil {
			reporter.Report(stats.Progress{Stage: stats.StageFailed})
		}
		return nil, err
	}

	// Collect stats if enabled
	if cfg != nil && cfg.Stats.Enabled {
		// Report collecting stats stage
		if reporter != nil {
			reporter.Report(stats.Progress{Stage: stats.StageCollectingStats})
		}

		if err := collectStatsWithProgress(ctx, s, dsn, cfg, reporter); err != nil {
			if reporter != nil {
				if err == clickhouse.ErrCancelled {
					reporter.Report(stats.Progress{Stage: stats.StageCancelled})
				} else {
					reporter.Report(stats.Progress{Stage: stats.StageFailed})
				}
			}
			return nil, err
		}

		// Run inference if enabled
		if cfg.Stats.Inference.Enabled {
			// Report inference stage
			if reporter != nil {
				reporter.Report(stats.Progress{Stage: stats.StageInferring})
			}

			opts := &schema.InferenceOptions{
				EnumMaxCardinality:      cfg.Stats.Inference.EnumMaxCardinality,
				EnumMaxDistinct:         cfg.Stats.Inference.EnumMaxDistinct,
				DictMaxCardinality:      cfg.Stats.Inference.DictMaxCardinality,
				DictMaxDistinct:         cfg.Stats.Inference.DictMaxDistinct,
				ForeignKeyMinConfidence: cfg.Stats.Inference.ForeignKeyMinConfidence,
			}
			inferrer := schema.NewInferrer(opts)
			if err := inferrer.RunInference(s); err != nil {
				if reporter != nil {
					reporter.Report(stats.Progress{Stage: stats.StageFailed})
				}
				return nil, err
			}
		}
	}

	// Report completed
	if reporter != nil {
		reporter.Report(stats.Progress{Stage: stats.StageCompleted})
	}

	return s, nil
}

func collectStatsWithProgress(ctx context.Context, s *schema.Schema, dsn config.DSN, cfg *config.Config, reporter stats.ProgressReporter) error {
	urlstr := dsn.URL

	// Setup checkpoint if enabled
	// Use interface types to avoid nil pointer vs nil interface issues
	var checkpointAdapter drivers.CheckpointUpdater
	var progressAdapter drivers.ProgressReporter

	if cfg.Stats.Checkpoint.Enabled {
		ttl, err := time.ParseDuration(cfg.Stats.Checkpoint.TTL)
		if err != nil {
			ttl = 24 * time.Hour
		}

		cpManager := stats.NewCheckpointManager(cfg.DocPath, ttl, cfg.Stats.Checkpoint.Force)
		dsnHash := stats.HashDSN(dsn.URL)
		schemaHash := stats.HashSchema(s)

		// Try to load existing checkpoint
		cp, err := cpManager.Load(dsnHash, schemaHash)
		if err != nil {
			// Log warning but continue
			cp = nil
		}

		if cp != nil {
			if cp.Stage == stats.StageCompleted {
				// Cache hit - use cached stats, skip collection
				logrus.WithField("cached_at", cp.UpdatedAt.Format(time.RFC3339)).Info("Using cached stats")
				stats.ApplyCheckpoint(s, cp)
				return nil
			}
			// Resume from checkpoint - apply partial results
			stats.ApplyCheckpoint(s, cp)
		} else {
			// Create new checkpoint
			cp = &stats.Checkpoint{
				DSNHash:    dsnHash,
				SchemaHash: schemaHash,
				Stage:      stats.StageCollectingStats,
			}
		}

		checkpointAdapter = stats.NewCheckpointAdapter(cpManager, cp)
	}

	if reporter != nil {
		progressAdapter = stats.NewProgressAdapter(reporter)
	}

	// Use stats-specific include/exclude if set, otherwise fall back to top-level config
	statsInclude := cfg.Stats.Include
	if len(statsInclude) == 0 {
		statsInclude = cfg.Include
	}
	statsExclude := cfg.Stats.Exclude
	if len(statsExclude) == 0 {
		statsExclude = cfg.Exclude
	}

	statsCfg := drivers.StatsConfig{
		Include:             statsInclude,
		Exclude:             statsExclude,
		TopN:                cfg.Stats.TopN,
		SampleSize:          cfg.Stats.SampleSize,
		LargeTableThreshold: cfg.Stats.LargeTableThreshold,
		RecentDays:          cfg.Stats.RecentDays,
		Progress:            progressAdapter,
		Checkpoint:          checkpointAdapter,
		DateColumn:          cfg.Stats.DateColumn,
		Tables:              convertTableStatsConfig(cfg.Stats.Tables),
		Ctx:                 ctx,
	}

	var collectErr error

	// Handle ClickHouse HTTP protocol
	if strings.HasPrefix(urlstr, "clickhouse+http://") || strings.HasPrefix(urlstr, "clickhouse+https://") {
		httpURL := strings.TrimPrefix(urlstr, "clickhouse+")
		db, err := clickhouse.OpenHTTP(httpURL)
		if err != nil {
			return err
		}
		defer db.Close()

		driver := clickhouse.New(db)
		collectErr = driver.CollectStats(s, statsCfg)
	} else {
		// Handle other drivers via dburl
		u, err := dburl.Parse(urlstr)
		if err != nil {
			return err
		}

		switch u.Driver {
		case "clickhouse":
			db, err := dburl.Open(urlstr)
			if err != nil {
				return err
			}
			defer db.Close()

			driver := clickhouse.New(db)
			collectErr = driver.CollectStats(s, statsCfg)
		default:
			// Stats not supported for this driver
			return nil
		}
	}

	// Mark checkpoint as completed on success (for cache reuse)
	if collectErr == nil && checkpointAdapter != nil {
		checkpointAdapter.MarkCompleted()
		_ = checkpointAdapter.Save()
	}

	return collectErr
}

// AnalyzeClickHouseHTTP analyzes ClickHouse database using HTTP protocol
// DSN format: clickhouse+http://user:password@host:port/database
// or clickhouse+https://user:password@host:port/database
func AnalyzeClickHouseHTTP(urlstr string) (*schema.Schema, error) {
	return AnalyzeClickHouseHTTPContext(context.Background(), urlstr)
}

// AnalyzeClickHouseHTTPContext analyzes ClickHouse database using HTTP protocol with context support
func AnalyzeClickHouseHTTPContext(ctx context.Context, urlstr string) (_ *schema.Schema, err error) {
	defer func() {
		err = errors.WithStack(err)
	}()

	// Convert clickhouse+http:// to http:// for clickhouse-go driver
	var httpURL string
	if strings.HasPrefix(urlstr, "clickhouse+https://") {
		httpURL = strings.TrimPrefix(urlstr, "clickhouse+")
	} else {
		httpURL = strings.TrimPrefix(urlstr, "clickhouse+")
	}

	// Parse URL to extract database name
	u, err := url.Parse(httpURL)
	if err != nil {
		return nil, fmt.Errorf("invalid ClickHouse HTTP DSN: %w", err)
	}

	dbName := strings.TrimPrefix(u.Path, "/")
	if dbName == "" {
		return nil, fmt.Errorf("database name is required in DSN: %s", urlstr)
	}

	s := &schema.Schema{
		Name: dbName,
	}

	// Open connection using clickhouse-go with HTTP protocol
	db, err := clickhouse.OpenHTTP(httpURL)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to connect to ClickHouse via HTTP: %w", err)
	}

	driver := clickhouse.New(db)
	if err := driver.Analyze(ctx, s); err != nil {
		return nil, err
	}

	return s, nil
}

// AnalyzeHTTPResource analyze `https://` or `http://`
func AnalyzeHTTPResource(dsn config.DSN) (_ *schema.Schema, err error) {
	defer func() {
		err = errors.WithStack(err)
	}()
	s := &schema.Schema{}
	req, err := http.NewRequest("GET", dsn.URL, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range dsn.Headers {
		req.Header.Add(k, v)
	}
	client := &http.Client{Timeout: time.Duration(10) * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(s); err != nil {
		return nil, err
	}
	if err := s.Repair(); err != nil {
		return nil, err
	}
	return s, nil
}

// AnalyzeGitHubContent analyze `github://`
func AnalyzeGitHubContent(dsn config.DSN) (_ *schema.Schema, err error) {
	defer func() {
		err = errors.WithStack(err)
	}()
	splitted := strings.SplitN(strings.TrimPrefix(dsn.URL, "github://"), "/", 3)
	if len(splitted) != 3 {
		return nil, fmt.Errorf("invalid dsn: %s", dsn)
	}
	s := &schema.Schema{}
	options := []factory.Option{factory.OwnerRepo(splitted[0] + "/" + splitted[1])}
	c, err := factory.NewGithubClient(options...)
	if err != nil {
		return nil, err
	}
	o := ghfs.Client(c)
	fsys, err := ghfs.New(splitted[0], splitted[1], o)
	if err != nil {
		return nil, err
	}
	b, err := fsys.ReadFile(splitted[2])
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	if err := dec.Decode(s); err != nil {
		return nil, err
	}
	if err := s.Repair(); err != nil {
		return nil, err
	}
	return s, nil
}

// AnalyzeJSON analyze `json://`
func AnalyzeJSON(urlstr string) (_ *schema.Schema, err error) {
	defer func() {
		err = errors.WithStack(err)
	}()
	s := &schema.Schema{}
	splitted := strings.Split(urlstr, "json://")
	file, err := os.Open(splitted[1])
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(file)
	if err := dec.Decode(s); err != nil {
		return nil, err
	}
	if err := s.Repair(); err != nil {
		return nil, err
	}
	return s, nil
}

// Deprecated.
func AnalyzeJSONString(str string) (*schema.Schema, error) {
	return AnalyzeJSONStringOrFile(str)
}

// AnalyzeJSONStringOrFile analyze JSON string or JSON file.
func AnalyzeJSONStringOrFile(strOrPath string) (s *schema.Schema, err error) {
	defer func() {
		err = errors.WithStack(err)
	}()
	s = &schema.Schema{}
	var buf io.Reader
	if strings.HasPrefix(strOrPath, "{") {
		buf = bytes.NewBufferString(strOrPath)
	} else {
		buf, err = os.Open(filepath.Clean(strOrPath))
		if err != nil {
			return nil, err
		}
	}
	dec := json.NewDecoder(buf)
	if err := dec.Decode(s); err != nil {
		return nil, err
	}
	if err := s.Repair(); err != nil {
		return nil, err
	}
	return s, nil
}

// AnalyzeWithExtDriver analyze with external driver command.
func AnalyzeWithExtDriver(urlstr string) (*schema.Schema, error) {
	u, err := url.Parse(urlstr)
	if err != nil {
		return nil, err
	}
	scheme := u.Scheme
	bin, err := safeexec.LookPath(fmt.Sprintf("tbls-driver-%s", scheme))
	if err != nil {
		return nil, fmt.Errorf("unsupported driver '%s'", scheme)
	}
	envs := os.Environ()
	envs = append(envs, fmt.Sprintf("TBLS_DSN=%s", urlstr))
	c := exec.Command(bin)
	buf := new(bytes.Buffer)
	c.Stdout = buf
	c.Stderr = os.Stderr
	c.Env = envs
	if err := c.Run(); err != nil {
		return nil, err
	}
	s := &schema.Schema{}
	dec := json.NewDecoder(buf)
	if err := dec.Decode(s); err != nil {
		return nil, err
	}
	if err := s.Repair(); err != nil {
		return nil, err
	}
	return s, nil
}

// convertTableStatsConfig converts config.TableStatsConfig to drivers.TableStatsConfig
func convertTableStatsConfig(tables map[string]config.TableStatsConfig) map[string]drivers.TableStatsConfig {
	if tables == nil {
		return nil
	}
	result := make(map[string]drivers.TableStatsConfig)
	for k, v := range tables {
		result[k] = drivers.TableStatsConfig{
			DateColumn: v.DateColumn,
			Skip:       v.Skip,
		}
	}
	return result
}
