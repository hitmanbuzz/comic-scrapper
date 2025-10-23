package system

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func CreateContext() *context.Context {
	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()	

	return &ctx
}

func SetupSignalHandler(cancel context.CancelFunc, logger *slog.Logger) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		logger.Info("received signal, shutting down", "signal", sig)
		cancel()

		// Give some time for graceful shutdown
		time.Sleep(2 * time.Second)
		os.Exit(0)
	}()
}
