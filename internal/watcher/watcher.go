package watcher

import (
	"context"
	"fmt"

	"github.com/fsnotify/fsnotify"
)

// Watcher goroutine for config changes using fsnotify
func WatchChanges(ctx context.Context, path string) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create watcher: %w", err)
	}
	defer watcher.Close()

	if err := watcher.Add(path); err != nil {
		return fmt.Errorf("failed to add watch on path %s: %w", path, err)
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			if event.Op&fsnotify.Write == fsnotify.Write {
				fmt.Println("config has changed!!")
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			fmt.Println("watch error:", err)
		}
	}
}
