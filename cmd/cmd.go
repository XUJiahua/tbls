package cmd

import (
	"github.com/k1LoW/tbls/config"
	"github.com/k1LoW/tbls/datasource"
	"github.com/k1LoW/tbls/schema"
)

func getSchemaFromJSONorDSN(c *config.Config) (*schema.Schema, error) {
	s, err := datasource.Analyze(c.DSN)
	if err != nil {
		return nil, err
	}
	if err := c.ModifySchema(s); err != nil {
		return nil, err
	}
	return s, nil
}

func getSchemaFromJSONorDSNWithStats(c *config.Config) (*schema.Schema, error) {
	s, err := datasource.AnalyzeWithStats(c.DSN, c)
	if err != nil {
		return nil, err
	}
	if err := c.ModifySchema(s); err != nil {
		return nil, err
	}
	return s, nil
}
