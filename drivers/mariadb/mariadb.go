package mariadb

import (
	"context"
	"database/sql"

	"github.com/k1LoW/tbls/drivers"
	"github.com/k1LoW/tbls/drivers/mysql"
	"github.com/k1LoW/tbls/schema"
)

type Mariadb struct {
	mysql.Mysql
}

// New return new Mariadb.
func New(db *sql.DB, opts ...drivers.Option) (*Mariadb, error) {
	m, err := mysql.New(db, opts...)
	if err != nil {
		return nil, err
	}
	m.EnableMariaMode()
	return &Mariadb{*m}, nil
}

// Analyze MariaDB database schema.
func (m *Mariadb) Analyze(ctx context.Context, s *schema.Schema) error {
	return m.Mysql.Analyze(ctx, s)
}
