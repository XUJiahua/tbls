package config

import (
	"io"

	"github.com/goccy/go-yaml"
	"github.com/k1LoW/errors"
	"github.com/k1LoW/tbls/config"
	"github.com/k1LoW/tbls/schema"
)

// Config struct for `tbls out`.
type Config struct {
	config *config.Config
}

// New return Config.
func New(c *config.Config) *Config {
	return &Config{
		config: c,
	}
}

func (c *Config) OutputSchema(wr io.Writer, _ *schema.Schema) error {
	d := yaml.NewEncoder(wr)
	defer d.Close()
	if err := d.Encode(c.config); err != nil {
		return errors.WithStack(err)
	}
	return nil
}

func (c *Config) OutputTable(_ io.Writer, _ *schema.Table) error {
	return errors.New("not supported")
}
