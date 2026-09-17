package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestBackoffDelayGrowsAndStopsAtMaximum(t *testing.T) {
	policy := retryPolicy{MaxAttempts: 6, InitialBackoff: 10 * time.Millisecond, MaxBackoff: 25 * time.Millisecond}
	want := []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 25 * time.Millisecond, 25 * time.Millisecond}
	for index, expected := range want {
		if got := backoffDelay(policy, index+1); got != expected {
			t.Fatalf("attempt %d: got %s, want %s", index+1, got, expected)
		}
	}
}

func TestQdrantRetryInterceptorRecoversFromTransientFailures(t *testing.T) {
	interceptor := qdrantRetryUnaryInterceptor(retryPolicy{MaxAttempts: 4})
	attempts := 0
	invoker := func(ctx context.Context, _ string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
		attempts++
		if _, ok := ctx.Deadline(); ok {
			t.Fatal("zero operation timeout unexpectedly added a deadline")
		}
		if attempts < 3 {
			return status.Error(codes.Unavailable, "synthetic restart")
		}
		return nil
	}
	if err := interceptor(context.Background(), "/qdrant.Points/Count", nil, nil, nil, invoker); err != nil {
		t.Fatalf("transient failure did not recover: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("got %d attempts, want 3", attempts)
	}
}

func TestQdrantRetryInterceptorDoesNotRetryAuthorizationFailure(t *testing.T) {
	interceptor := qdrantRetryUnaryInterceptor(retryPolicy{MaxAttempts: 4})
	attempts := 0
	err := interceptor(context.Background(), "/qdrant.Points/Count", nil, nil, nil,
		func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
			attempts++
			return status.Error(codes.PermissionDenied, "synthetic denial")
		})
	if status.Code(err) != codes.PermissionDenied || attempts != 1 {
		t.Fatalf("authorization failure should be returned immediately: attempts=%d err=%v", attempts, err)
	}
}

func TestQdrantRetryBackoffHonorsContextCancellation(t *testing.T) {
	interceptor := qdrantRetryUnaryInterceptor(retryPolicy{MaxAttempts: 4, InitialBackoff: time.Second, MaxBackoff: time.Second})
	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0
	err := interceptor(ctx, "/qdrant.Points/Count", nil, nil, nil,
		func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
			attempts++
			time.AfterFunc(10*time.Millisecond, cancel)
			return status.Error(codes.Unavailable, "synthetic restart")
		})
	if !errors.Is(err, context.Canceled) || attempts != 1 {
		t.Fatalf("retry did not stop on cancellation: attempts=%d err=%v", attempts, err)
	}
}

func TestEmbeddingModelDigestRetriesTransientHTTPFailure(t *testing.T) {
	worker := &IngestionWorker{
		Cfg:        Config{OllamaHost: "http://127.0.0.1:11434", EmbeddingModel: "embed-model"},
		HTTPClient: &http.Client{},
	}
	attempts := 0
	worker.HTTPClient.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader("restarting"))}, nil
		}
		body := `{"models":[{"name":"embed-model:latest","digest":"0123456789abcdef"}]}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	digest, err := worker.fetchEmbeddingModelDigest(context.Background())
	if err != nil || digest != "0123456789abcdef" || attempts != 2 {
		t.Fatalf("model digest did not recover: digest=%q attempts=%d err=%v", digest, attempts, err)
	}
}

func TestEmbeddingModelDigestDoesNotRetryPermanentHTTPFailure(t *testing.T) {
	worker := &IngestionWorker{
		Cfg:        Config{OllamaHost: "http://127.0.0.1:11434", EmbeddingModel: "embed-model"},
		HTTPClient: &http.Client{},
	}
	attempts := 0
	worker.HTTPClient.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
		attempts++
		return &http.Response{StatusCode: http.StatusBadRequest, Body: io.NopCloser(strings.NewReader("bad request"))}, nil
	})
	if _, err := worker.fetchEmbeddingModelDigest(context.Background()); err == nil || attempts != 1 {
		t.Fatalf("permanent HTTP failure should not be retried: attempts=%d err=%v", attempts, err)
	}
}
