package cancellation

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

type networkCanceled struct{}

func (networkCanceled) Error() string       { return "operation was canceled" }
func (networkCanceled) Is(other error) bool { return other == context.Canceled }

func TestIs(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"direct", context.Canceled, true},
		{"wrapped", fmt.Errorf("operation: %w", context.Canceled), true},
		{"network", &net.OpError{Op: "dial", Net: "udp", Err: networkCanceled{}}, true},
		{"joined_single", errors.Join(context.Canceled), true},
		{"joined_canceled", errors.Join(context.Canceled, fmt.Errorf("lookup: %w", networkCanceled{})), true},
		{"nested_mixed", fmt.Errorf("connect: %w", errors.Join(context.Canceled, errors.New("refused"))), false},
		{"mixed_reversed", errors.Join(errors.New("refused"), context.Canceled), false},
		{"joined_deadline", errors.Join(context.Canceled, context.DeadlineExceeded), false},
		{"deadline", context.DeadlineExceeded, false},
		{"unrelated", errors.New("operation was canceled"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Is(ctx, tc.err); got != tc.want {
				t.Fatalf("Is = %v, want %v", got, tc.want)
			}
			if Is(t.Context(), tc.err) {
				t.Fatal("active context classified as canceled")
			}
			expired, stop := context.WithTimeout(t.Context(), 0)
			defer stop()
			if Is(expired, tc.err) {
				t.Fatal("deadline classified as cancellation")
			}
		})
	}
}

func TestPGXLookupCancellation(t *testing.T) {
	for _, mixed := range []bool{false, true} {
		t.Run(fmt.Sprintf("mixed=%v", mixed), func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			cfg, err := pgconn.ParseConfig("host=database.invalid user=test dbname=test sslmode=disable")
			if err != nil {
				t.Fatal(err)
			}
			resolver := &net.Resolver{PreferGo: true, Dial: func(context.Context, string, string) (net.Conn, error) {
				return nil, context.Canceled
			}}
			cfg.LookupFunc = func(ctx context.Context, host string) ([]string, error) {
				cancel()
				_, cause := resolver.LookupIPAddr(ctx, host)
				if !errors.Is(cause, context.Canceled) {
					t.Fatalf("expected resolver cancellation, got %v", cause)
				}
				if mixed {
					cause = errors.Join(cause, errors.New("DNS unavailable"))
				}
				return nil, cause
			}
			conn, err := pgconn.ConnectConfig(ctx, cfg)
			if conn != nil {
				_ = conn.Close(t.Context())
				t.Fatal("unexpected connection")
			}
			var connectErr *pgconn.ConnectError
			if !errors.As(err, &connectErr) {
				t.Fatalf("expected pgx ConnectError, got %T", err)
			}
			if got := Is(ctx, err); got == mixed {
				t.Fatalf("Is = %v, error %v", got, err)
			}
		})
	}
}
