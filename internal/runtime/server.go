package runtime

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// Serve waits for Shutdown to finish before returning. Exiting immediately when
// ListenAndServe returns ErrServerClosed would interrupt requests still draining.
func Serve(ctx context.Context, addr string, handler http.Handler, logger *slog.Logger) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return ServeListener(ctx, listener, handler, logger)
}

func ServeListener(ctx context.Context, listener net.Listener, handler http.Handler, logger *slog.Logger) error {
	addr := listener.Addr().String()
	server := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
	result := make(chan error, 1)
	go func() { result <- server.Serve(listener) }()
	logger.Info("server ready", "address", addr)
	select {
	case err := <-result:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			server.Close()
			return err
		}
		err := <-result
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
