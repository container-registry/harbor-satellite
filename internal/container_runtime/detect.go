package runtime

import (
	"os"
	"os/exec"
)

// CRIType identifies a container runtime.
type CRIType string

const (
	CRIDocker     CRIType = "docker"
	CRIContainerd CRIType = "containerd"
	CRICrio       CRIType = "crio"
	CRIPodman     CRIType = "podman"
)

// DetectedCRI holds the detected CRI type and the reason it was detected.
type DetectedCRI struct {
	Type   CRIType
	Reason string
}

type criCheck struct {
	criType CRIType
	socket  string
	binary  string
}

var criChecks = []criCheck{
	{CRIDocker, "/var/run/docker.sock", "docker"},
	{CRIContainerd, "/run/containerd/containerd.sock", "containerd"},
	{CRICrio, "/var/run/crio/crio.sock", "crio"},
	{CRIPodman, "", "podman"},
}

// statFunc matches os.Stat signature.
type statFunc func(string) (os.FileInfo, error)

// lookPathFunc matches exec.LookPath signature.
type lookPathFunc func(string) (string, error)

// DetectInstalledCRIs returns all CRIs found on the system.
func DetectInstalledCRIs() []DetectedCRI {
	return detectWithCheckers(os.Stat, exec.LookPath)
}

func detectWithCheckers(statFn statFunc, lookPathFn lookPathFunc) []DetectedCRI {
	var detected []DetectedCRI

	for _, check := range criChecks {
		if check.socket != "" {
			if _, err := statFn(check.socket); err == nil {
				detected = append(detected, DetectedCRI{
					Type:   check.criType,
					Reason: "found socket " + check.socket,
				})
				continue
			}
		}

		if _, err := lookPathFn(check.binary); err == nil {
			detected = append(detected, DetectedCRI{
				Type:   check.criType,
				Reason: "found binary " + check.binary,
			})
		}
	}

	return detected
}
