package store

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/nerdswhofish/coop/internal/cancellation"
	"gorm.io/gorm"
	"gorm.io/gorm/callbacks"
)

func installTransactionCallbacks(db *gorm.DB) error {
	for _, replace := range []func(string, func(*gorm.DB)) error{
		db.Callback().Create().Replace,
		db.Callback().Update().Replace,
		db.Callback().Delete().Replace,
	} {
		if err := replace("gorm:commit_or_rollback_transaction", finishTransaction); err != nil {
			return fmt.Errorf("installing transaction cleanup: %w", err)
		}
	}
	return nil
}

func finishTransaction(db *gorm.DB) {
	original := db.Error
	canceled := cancellation.Is(db.Statement.Context, original)
	callbacks.CommitOrRollbackTransaction(db)
	// Cancellation can roll back the SQL transaction or close its pgx connection.
	// GORM's AddError then wraps only the cleanup error, discarding the query cause.
	if canceled && transactionClosed(errors.Unwrap(db.Error)) {
		db.Error = original
		if db.Statement.Result != nil {
			db.Statement.Result.Error = original
		}
	}
}

func transactionClosed(err error) bool {
	for err != nil {
		if err == sql.ErrTxDone || err == pgconn.ErrConnClosed {
			return true
		}
		// A joined error may contain an independent cleanup failure.
		err = errors.Unwrap(err)
	}
	return false
}
