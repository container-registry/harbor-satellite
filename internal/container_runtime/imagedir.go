package runtime

import "os"

// Well-known image directories for Kubernetes distributions that support
// automatic tarball import via filesystem watching.
var imageDirectories = []string{
	"/var/lib/rancher/k3s/agent/images",
	"/var/lib/rancher/rke2/agent/images",
}

// DetectImageDir returns the first existing well-known image directory
// for k3s or RKE2. Returns an empty string if none is found.
func DetectImageDir() string {
	return detectImageDir(os.Stat)
}

func detectImageDir(stat statFunc) string {
	for _, dir := range imageDirectories {
		if info, err := stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}
	return ""
}
