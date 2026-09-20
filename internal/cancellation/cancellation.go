// Package cancellation identifies operations that failed solely because their caller canceled.
package cancellation

import (
	"context"
	"errors"
)

func Is(ctx context.Context, err error) bool {
	return ctx.Err() == context.Canceled && onlyCanceled(err)
}

func onlyCanceled(err error) bool {
	if err == nil {
		return false
	}
	switch wrapped := err.(type) {
	case interface{ Unwrap() []error }:
		// pgx joins connection attempts, even when there is only one cause.
		causes := wrapped.Unwrap()
		if len(causes) == 0 {
			return false
		}
		for _, cause := range causes {
			if !onlyCanceled(cause) {
				return false
			}
		}
		return true
	case interface{ Unwrap() error }:
		return onlyCanceled(wrapped.Unwrap())
	default:
		// net's cancellation sentinel matches through Is, not identity.
		return errors.Is(err, context.Canceled)
	}
}
