package server

import (
	"context"
	"testing"
	"time"
)

func TestMCPClientDisconnectEndsSession(t *testing.T) {
	done := make(chan struct{})
	var session context.Context
	go func() {
		waitMCPClient(context.Background(), func(ctx context.Context) { session = ctx })
		close(done)
	}()
	select {
	case <-done:
		if session.Err() != context.Canceled {
			t.Fatal("session context remained active after the client disconnected")
		}
	case <-time.After(time.Second):
		t.Fatal("client disconnect did not terminate the MCP session")
	}
}

func TestMCPCancellationDoesNotWaitForBlockedInput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reading := make(chan struct{})
	releaseInput := make(chan struct{})
	defer close(releaseInput)
	done := make(chan struct{})
	go func() {
		waitMCPClient(ctx, func(context.Context) {
			close(reading)
			<-releaseInput
		})
		close(done)
	}()
	<-reading
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown waited for input from a connected but idle client")
	}
}
