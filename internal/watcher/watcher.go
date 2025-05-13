package watcher

import (
	"context"
	"fmt"

	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog"
)

// Watcher goroutine for config changes using fsnotify
func WatchChanges(ctx context.Context, log zerolog.Logger, path string, eventChan chan<- struct{}) error {
	log.Info().Msg("Setting up watcher to watch for changes in config file")

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Error().Err(err).Msg("Failed to create fsnotify watcher")
		return fmt.Errorf("failed to create watcher: %w", err)
	}
	defer func() {
		_ = watcher.Close()
		log.Info().Msg("Stopped watching config file")
	}()

	if err := watcher.Add(path); err != nil {
		log.Error().Err(err).Str("path", path).Msg("Failed to add path to watcher")
		return fmt.Errorf("failed to add watch on path %s: %w", path, err)
	}

	log.Info().Str("path", path).Msg("Started watching config file for changes")

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("Watcher received context cancellation. Shutting down gracefully...")
			return nil
		case event, ok := <-watcher.Events:
			if !ok {
				log.Error().Msg("Watcher event channel closed unexpectedly")
				return nil
			}
			log.Debug().Str("event", event.String()).Msg("Received fsnotify event")
			if event.Op&fsnotify.Write == fsnotify.Write {
				log.Info().Str("file", event.Name).Msg("File modified")
				select {
				case eventChan <- struct{}{}:
					log.Debug().Msg("Notified config change event on channel")
				default:
					log.Warn().Msg("Config change channel full; skipping event")
				}
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				log.Error().Msg("Watcher error channel closed unexpectedly")
				return nil
			}
			log.Error().Err(err).Msg("Watcher encountered an error")
		}
	}
}
