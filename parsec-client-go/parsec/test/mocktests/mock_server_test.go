package mocktests_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/onsi/ginkgo"
)

type mockServer struct {
	cancel context.CancelFunc
}

func startMockServer() (*mockServer, error) {
	os.Setenv("PARSEC_SERVICE_ENDPOINT", "unix:"+parsecSocket)
	var pythonExe string
	pythonExe, err := exec.LookPath("python3")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	cmdRunMock := exec.CommandContext(ctx, pythonExe, mockServerPath, "--test-folder", mockServerData, "--parsec-socket", parsecSocket)
	cmdRunMock.Stdout = ginkgo.GinkgoWriter
	cmdRunMock.Stderr = ginkgo.GinkgoWriter

	fmt.Fprintf(ginkgo.GinkgoWriter, "Running: %s\n", cmdRunMock.String())
	err = cmdRunMock.Start()
	if err != nil {
		cancel()
		return nil, err
	}

	ms := &mockServer{
		cancel: cancel,
	}
	err = waitForServerSocket()
	if err != nil {
		cancel()
		return nil, err
	}
	return ms, nil
}

func waitForServerSocket() error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	for {
		if _, err := os.Stat(parsecSocket); err == nil {
			// parsec socket exists, exit
			fmt.Fprintf(ginkgo.GinkgoWriter, "found parsec socket\n")
			// need short wait till socket can connect
			time.Sleep(200 * time.Millisecond)
			return nil
		} else if errors.Is(err, os.ErrNotExist) {
			// do nothing, wait a bit or timeout
			select {
			case <-ctx.Done():
				fmt.Fprintf(ginkgo.GinkgoWriter, "Context done")
				return ctx.Err()
			case <-time.After(10 * time.Microsecond):
				continue
			}
		} else {
			fmt.Fprintf(ginkgo.GinkgoWriter, "error statting parsec socket")
			return err
		}
	}
}

func (ms *mockServer) stop() {
	ms.cancel()
}
