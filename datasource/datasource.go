package datasource

import (
	"bytes"
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
func Analyze(dsn config.DSN) (_ *schema.Schema, err error) {
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
		return AnalyzeClickHouseHTTP(urlstr)
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
	if err := db.Ping(); err != nil {
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
	err = driver.Analyze(s)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// AnalyzeWithStats analyzes database and optionally collects statistics
func AnalyzeWithStats(dsn config.DSN, cfg *config.Config) (*schema.Schema, error) {
	s, err := Analyze(dsn)
	if err != nil {
		return nil, err
	}

	// Collect stats if enabled
	if cfg != nil && cfg.Stats.Enabled {
		if err := collectStats(s, dsn, cfg); err != nil {
			// Log error but don't fail - stats are optional
			// TODO: consider adding proper logging
			_ = err
		}
	}

	return s, nil
}

func collectStats(s *schema.Schema, dsn config.DSN, cfg *config.Config) error {
	urlstr := dsn.URL

	statsCfg := drivers.StatsConfig{
		Include:             cfg.Stats.Include,
		Exclude:             cfg.Stats.Exclude,
		TopN:                cfg.Stats.TopN,
		SampleSize:          cfg.Stats.SampleSize,
		LargeTableThreshold: cfg.Stats.LargeTableThreshold,
		RecentDays:          cfg.Stats.RecentDays,
	}

	// Handle ClickHouse HTTP protocol
	if strings.HasPrefix(urlstr, "clickhouse+http://") || strings.HasPrefix(urlstr, "clickhouse+https://") {
		httpURL := strings.TrimPrefix(urlstr, "clickhouse+")
		db, err := clickhouse.OpenHTTP(httpURL)
		if err != nil {
			return err
		}
		defer db.Close()

		driver := clickhouse.New(db)
		return driver.CollectStats(s, statsCfg)
	}

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
		return driver.CollectStats(s, statsCfg)
	default:
		// Stats not supported for this driver
		return nil
	}
}

// AnalyzeClickHouseHTTP analyzes ClickHouse database using HTTP protocol
// DSN format: clickhouse+http://user:password@host:port/database
// or clickhouse+https://user:password@host:port/database
func AnalyzeClickHouseHTTP(urlstr string) (_ *schema.Schema, err error) {
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

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to connect to ClickHouse via HTTP: %w", err)
	}

	driver := clickhouse.New(db)
	if err := driver.Analyze(s); err != nil {
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
