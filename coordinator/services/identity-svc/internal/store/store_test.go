// Unit-level tests for the helpers WithTx relies on. The transactional
// path itself is covered by the integration suite (apple_concurrent_*),
// because retry-on-40001 only manifests against a real Postgres server.
package store

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsSerializationFailure(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"sqlstate 40001", &pgconn.PgError{Code: "40001"}, true},
		{"sqlstate 23505 (unique violation)", &pgconn.PgError{Code: "23505"}, false},
		{"sqlstate 42P01 (undefined table)", &pgconn.PgError{Code: "42P01"}, false},
		{"plain error", errors.New("boom"), false},
		{"nil", nil, false},
		{"wrapped 40001", errors.Join(errors.New("ctx"), &pgconn.PgError{Code: "40001"}), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSerializationFailure(tc.err); got != tc.want {
				t.Errorf("isSerializationFailure(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
