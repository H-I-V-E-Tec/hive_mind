package server

import (
	"context"
	"io"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type retryPolicy struct {
	MaxAttempts      int
	InitialBackoff   time.Duration
	MaxBackoff       time.Duration
	OperationTimeout time.Duration
}

var serviceRetryPolicy = retryPolicy{
	MaxAttempts:      4,
	InitialBackoff:   100 * time.Millisecond,
	MaxBackoff:       time.Second,
	OperationTimeout: 30 * time.Second,
}

func (p retryPolicy) normalized() retryPolicy {
	if p.MaxAttempts < 1 {
		p.MaxAttempts = 1
	}
	if p.InitialBackoff < 0 {
		p.InitialBackoff = 0
	}
	if p.MaxBackoff < p.InitialBackoff {
		p.MaxBackoff = p.InitialBackoff
	}
	return p
}

// backoffDelay returns a bounded exponential delay for the number of the
// attempt that just failed. A failed first attempt waits InitialBackoff.
func backoffDelay(policy retryPolicy, failedAttempt int) time.Duration {
	policy = policy.normalized()
	delay := policy.InitialBackoff
	for attempt := 1; attempt < failedAttempt && delay < policy.MaxBackoff; attempt++ {
		if delay > policy.MaxBackoff/2 {
			return policy.MaxBackoff
		}
		delay *= 2
	}
	if delay > policy.MaxBackoff {
		return policy.MaxBackoff
	}
	return delay
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func boundedRetryContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

func retryableGRPCError(ctx context.Context, err error) bool {
	if err == nil || ctx.Err() != nil {
		return false
	}
	switch status.Code(err) {
	case codes.Unavailable, codes.ResourceExhausted, codes.DeadlineExceeded:
		return true
	default:
		return false
	}
}

// qdrantRetryUnaryInterceptor keeps one bounded operation alive while gRPC
// reconnects its underlying channel and retries only transient status codes.
// Qdrant point writes used by this server are idempotent: point IDs and
// optimistic update filters are deterministic and verified after commit.
func qdrantRetryUnaryInterceptor(policy retryPolicy) grpc.UnaryClientInterceptor {
	policy = policy.normalized()
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		bounded, cancel := boundedRetryContext(ctx, policy.OperationTimeout)
		defer cancel()

		// WaitForReady lets an in-flight operation survive the channel's
		// CONNECTING/TRANSIENT_FAILURE states during a service restart.
		opts = append(opts, grpc.WaitForReady(true))
		var lastErr error
		for attempt := 1; attempt <= policy.MaxAttempts; attempt++ {
			lastErr = invoker(bounded, method, req, reply, cc, opts...)
			if lastErr == nil || !retryableGRPCError(bounded, lastErr) || attempt == policy.MaxAttempts {
				return lastErr
			}
			if err := waitForRetry(bounded, backoffDelay(policy, attempt)); err != nil {
				return err
			}
		}
		return lastErr
	}
}

func retryableHTTPStatus(code int) bool {
	switch code {
	case 408, 425, 429, 500, 502, 503, 504:
		return true
	default:
		return false
	}
}

func closeRetryResponse(body io.ReadCloser) {
	if body == nil {
		return
	}
	// Drain a bounded response so the HTTP transport can reuse the local
	// connection without accepting an unbounded error body.
	_, _ = io.Copy(io.Discard, io.LimitReader(body, 32<<10))
	_ = body.Close()
}
