package events

import (
	"sync"
	"testing"
)

func newTestRefreshProcess() *RefreshCredentialProcess {
	return &RefreshCredentialProcess{
		name:       "refresh_credentials",
		isRunning:  false,
		isComplete: false,
	}
}

func TestRefreshCredentialProcess_Name(t *testing.T) {
	p := newTestRefreshProcess()

	if got := p.Name(); got != "refresh_credentials" {
		t.Errorf("expected name %q, got %q", "refresh_credentials", got)
	}
}

func TestRefreshCredentialProcess_IsComplete_Default(t *testing.T) {
	p := newTestRefreshProcess()

	if p.IsComplete() {
		t.Error("expected IsComplete to be false by default")
	}

	p.isComplete = true
	if !p.IsComplete() {
		t.Error("expected IsComplete to reflect true after being set")
	}
}

func TestRefreshCredentialProcess_IsRunning_Default(t *testing.T) {
	p := newTestRefreshProcess()

	if p.IsRunning() {
		t.Error("expected IsRunning to be false by default")
	}
}

func TestRefreshCredentialProcess_StartStop(t *testing.T) {
	p := newTestRefreshProcess()

	p.start()
	if !p.IsRunning() {
		t.Error("expected IsRunning to be true after start()")
	}

	p.stop()
	if p.IsRunning() {
		t.Error("expected IsRunning to be false after stop()")
	}
}

func TestRefreshCredentialProcess_StartStop_Concurrent(t *testing.T) {
	p := newTestRefreshProcess()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			p.start()
		}()
		go func() {
			defer wg.Done()
			_ = p.IsRunning()
		}()
	}
	wg.Wait()
	p.stop()

	if p.IsRunning() {
		t.Error("expected IsRunning to be false after final stop()")
	}
}
