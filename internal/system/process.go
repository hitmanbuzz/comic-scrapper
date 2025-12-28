package system

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func SetupSignalHandler(cancel context.CancelFunc, logger *slog.Logger, shutdownCh chan<- struct{}) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		logger.Info("received signal, shutting down", "signal", sig)
		cancel()

		// Give some time for graceful shutdown
		time.Sleep(2 * time.Second)

		// Signal that shutdown is complete
		select {
		case shutdownCh <- struct{}{}:
		default:
		}
	}()
}
