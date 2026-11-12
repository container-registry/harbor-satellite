package watcher

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog"
)

// Watcher goroutine for config changes using fsnotify.
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

	// Watching the file itself loses the watch when an editor or a tool
	// renames a new file over it; the directory watch survives that.
	file := filepath.Clean(path)
	if err := watcher.Add(filepath.Dir(file)); err != nil {
		log.Error().Err(err).Str("path", path).Msg("Failed to add path to watcher")
		return fmt.Errorf("failed to add watch on path %s: %w", path, err)
	}

	log.Info().Str("path", path).Msg("Started watching config file for changes")

	// out is eventChan while a change is waiting for the consumer and nil
	// otherwise. Changes that arrive meanwhile merge into the waiting one,
	// so a consumer busy with a reload still sees the last write.
	var out chan<- struct{}
	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("Watcher received context cancellation. Shutting down gracefully...")
			return nil
		case out <- struct{}{}:
			out = nil
			log.Debug().Msg("Notified config change event on channel")
		case event, ok := <-watcher.Events:
			if !ok {
				log.Error().Msg("Watcher event channel closed unexpectedly")
				return nil
			}
			log.Debug().Str("event", event.String()).Msg("Received fsnotify event")
			if filepath.Clean(event.Name) != file {
				continue
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
				log.Info().Str("file", event.Name).Msg("File modified")
				out = eventChan
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
