package runtime

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestGracefulShutdownWaitsForActiveRequest(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-release; w.Write([]byte("finished")) })
	go func() { done <- ServeListener(ctx, listener, handler, slog.New(slog.NewTextHandler(io.Discard, nil))) }()
	response := make(chan error, 1)
	go func() {
		res, err := http.Get("http://" + listener.Addr().String())
		if err == nil {
			data, e := io.ReadAll(res.Body)
			res.Body.Close()
			if e != nil {
				err = e
			} else if string(data) != "finished" {
				err = io.ErrUnexpectedEOF
			}
		}
		response <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("request never started")
	}
	cancel()
	select {
	case err := <-done:
		t.Fatalf("shutdown returned before request finished: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	select {
	case err := <-response:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("response interrupted")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown did not finish")
	}
}
