package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/nerdswhofish/coop/internal/cancellation"
	"gorm.io/gorm"
)

type cleanupTransaction struct {
	gorm.ConnPool
	rollbackErr error
	commitErr   error
	rollbacks   int
	commits     int
}

func (tx *cleanupTransaction) Rollback() error { tx.rollbacks++; return tx.rollbackErr }
func (tx *cleanupTransaction) Commit() error   { tx.commits++; return tx.commitErr }

func TestTransactionCleanupPreservesOnlyConfirmedCancellation(t *testing.T) {
	independent := errors.New("storage failed")
	for _, tc := range []struct {
		name             string
		original         error
		cleanup          error
		canceled         bool
		wantCancellation bool
	}{
		{"automatic_rollback", fmt.Errorf("query: %w", context.Canceled), sql.ErrTxDone, true, true},
		{"closed_connection", context.Canceled, fmt.Errorf("deallocating: %w", pgconn.ErrConnClosed), true, true},
		{"mixed_closed_connection", context.Canceled, errors.Join(pgconn.ErrConnClosed, independent), true, false},
		{"commit_closed_connection", nil, pgconn.ErrConnClosed, true, false},
		{"normal_rollback", context.Canceled, nil, true, true},
		{"active_context", context.Canceled, sql.ErrTxDone, false, false},
		{"query_failure", independent, sql.ErrTxDone, true, false},
		{"query_deadline", context.DeadlineExceeded, sql.ErrTxDone, true, false},
		{"mixed_query_failure", errors.Join(context.Canceled, independent), sql.ErrTxDone, true, false},
		{"cleanup_failure", context.Canceled, independent, true, false},
		{"mixed_cleanup_failure", context.Canceled, errors.Join(sql.ErrTxDone, independent), true, false},
		{"commit_failure", nil, sql.ErrTxDone, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tc.canceled {
				cancel()
			}
			tx := &cleanupTransaction{rollbackErr: tc.cleanup, commitErr: tc.cleanup}
			pool := &cleanupTransaction{}
			db := &gorm.DB{Config: &gorm.Config{ConnPool: pool}, Error: tc.original}
			db.Statement = &gorm.Statement{DB: db, Context: ctx, ConnPool: tx, Result: gorm.WithResult()}
			db.Statement.Result.Error = tc.original
			db.InstanceSet("gorm:started_transaction", true)
			finishTransaction(db)
			if got := cancellation.Is(ctx, db.Error); got != tc.wantCancellation {
				t.Fatalf("cancellation=%v want=%v error=%v", got, tc.wantCancellation, db.Error)
			}
			if tc.wantCancellation && db.Error != tc.original {
				t.Fatal("original cause not retained")
			}
			if tc.cleanup != nil && !tc.wantCancellation && !errors.Is(db.Error, tc.cleanup) {
				t.Fatalf("cleanup error lost: %v", db.Error)
			}
			if db.Statement.Result.Error != db.Error {
				t.Fatal("result error differs from DB error")
			}
			if db.Statement.ConnPool != pool {
				t.Fatal("transaction connection not released")
			}
			if tc.original == nil {
				if tx.commits != 1 || tx.rollbacks != 0 {
					t.Fatal("commit path changed")
				}
			} else if tx.rollbacks != 1 || tx.commits != 0 {
				t.Fatal("rollback path changed")
			}
		})
	}
}
