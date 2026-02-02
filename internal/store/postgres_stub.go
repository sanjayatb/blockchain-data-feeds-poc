//go:build !postgres

package store

import "fmt"

func NewPostgres(dsn string) (Store, error) {
	return nil, fmt.Errorf("postgres support not enabled (build with -tags=postgres)")
}
