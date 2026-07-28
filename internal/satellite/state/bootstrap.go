package state

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/container-registry/harbor-satellite/internal/logger"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// BootstrapOptions holds configuration for the bootstrap process.
type BootstrapOptions struct {
	ArtifactPath      string
	ConfigDir         string
	StateFilePath     string
	RegistryURL       string
	RegistryUsername  string
	RegistryPassword  string
	UseUnsecure       bool
}

// BootstrapSatellite loads OCI layout or tarball archive and pushes images to the local registry.
func BootstrapSatellite(ctx context.Context, opts BootstrapOptions) error {
	log := logger.FromContext(ctx)
	log.Info().Str("artifact", opts.ArtifactPath).Msg("Starting satellite image bootstrap process")

	// Detect if artifact is a directory or file
	info, err := os.Stat(opts.ArtifactPath)
	if err != nil {
		return fmt.Errorf("failed to access artifact path %s: %w", opts.ArtifactPath, err)
	}

	var ociDir string
	var tempDir string

	if info.IsDir() {
		ociDir = opts.ArtifactPath
		log.Info().Msgf("Detected OCI layout directory at %s", ociDir)
	} else {
		// Create temporary directory for extraction
		tempDir, err = os.MkdirTemp("", "satellite-bootstrap-*")
		if err != nil {
			return fmt.Errorf("failed to create temp directory for extraction: %w", err)
		}
		defer func() {
			if tempDir != "" {
				log.Debug().Str("path", tempDir).Msg("Cleaning up temp directory")
				_ = os.RemoveAll(tempDir)
			}
		}()

		log.Info().Msgf("Detected archive file. Extracting %s to %s", opts.ArtifactPath, tempDir)
		if err := extractTar(opts.ArtifactPath, tempDir); err != nil {
			return fmt.Errorf("failed to extract tarball: %w", err)
		}
		ociDir = tempDir
	}

	// Load OCI layout
	ociLayout, err := layout.FromPath(ociDir)
	if err != nil {
		return fmt.Errorf("failed to load OCI layout from path %s: %w", ociDir, err)
	}

	index, err := ociLayout.ImageIndex()
	if err != nil {
		return fmt.Errorf("failed to read OCI layout index: %w", err)
	}

	indexManifest, err := index.IndexManifest()
	if err != nil {
		return fmt.Errorf("failed to parse index manifest: %w", err)
	}

	pushAuth := authn.FromConfig(authn.AuthConfig{
		Username: opts.RegistryUsername,
		Password: opts.RegistryPassword,
	})

	var nameOpts []name.Option
	pushOpts := []remote.Option{remote.WithAuth(pushAuth), remote.WithContext(ctx)}

	if opts.UseUnsecure {
		nameOpts = append(nameOpts, name.Insecure)
	}

	var entities []Entity

	for i, manifestDesc := range indexManifest.Manifests {
		// Read reference name from annotations (standard: org.opencontainers.image.ref.name)
		refName := manifestDesc.Annotations["org.opencontainers.image.ref.name"]
		if refName == "" {
			// Fallback: generate a dummy name using digest
			refName = fmt.Sprintf("library/bootstrap:%s", manifestDesc.Digest.Hex[:12])
			log.Warn().Msgf("No reference name annotation found for manifest %s. Using fallback: %s", manifestDesc.Digest.String(), refName)
		}

		log.Info().Msgf("Loading image/index [%d/%d]: %s", i+1, len(indexManifest.Manifests), refName)

		// Parse target reference on local registry
		dstRefStr := fmt.Sprintf("%s/%s", opts.RegistryURL, refName)
		dstRef, err := name.ParseReference(dstRefStr, nameOpts...)
		if err != nil {
			return fmt.Errorf("failed to parse destination ref %s: %w", dstRefStr, err)
		}

		// Pull image/index from OCI layout and push to registry
		img, err := index.Image(manifestDesc.Digest)
		if err == nil {
			// Validate integrity
			_, err = img.Manifest()
			if err != nil {
				return fmt.Errorf("manifest integrity check failed for %s: %w", refName, err)
			}
			log.Info().Msgf("Pushing image %s to local registry", refName)
			if err := remote.Write(dstRef, img, pushOpts...); err != nil {
				return fmt.Errorf("failed to push image %s: %w", refName, err)
			}
		} else {
			idx, err := index.ImageIndex(manifestDesc.Digest)
			if err != nil {
				return fmt.Errorf("failed to retrieve index or image for %s: %w", refName, err)
			}
			// Validate integrity
			_, err = idx.IndexManifest()
			if err != nil {
				return fmt.Errorf("index manifest integrity check failed for %s: %w", refName, err)
			}
			log.Info().Msgf("Pushing index %s to local registry", refName)
			if err := remote.WriteIndex(dstRef, idx, pushOpts...); err != nil {
				return fmt.Errorf("failed to push index %s: %w", refName, err)
			}
		}

		// Split the reference into repository and image name
		// library/alpine:latest -> repo=library, name=alpine, tag=latest
		parsedRef, err := name.ParseReference(refName)
		if err != nil {
			return fmt.Errorf("failed to parse OCI ref %s: %w", refName, err)
		}

		repoPath := parsedRef.Context().RepositoryStr()
		tagValue := parsedRef.Identifier()

		parts := strings.Split(repoPath, "/")
		var repo, imgName string
		if len(parts) >= 2 {
			repo = parts[0]
			imgName = strings.Join(parts[1:], "/")
		} else {
			repo = "library"
			imgName = repoPath
		}

		entities = append(entities, Entity{
			Name:       imgName,
			Repository: repo,
			Tag:        tagValue,
			Digest:     manifestDesc.Digest.String(),
		})
	}

	// Generate and write state.json
	stateMap := []StateMap{
		{
			url:      "offline-bootstrap",
			Entities: entities,
		},
	}

	log.Info().Str("path", opts.StateFilePath).Msg("Writing state.json from bootstrapped images")
	if err := SaveState(opts.StateFilePath, stateMap, ""); err != nil {
		return fmt.Errorf("failed to save bootstrap state: %w", err)
	}

	log.Info().Msg("Bootstrap completed successfully")
	return nil
}

// extractTar extracts a tar file to the destination directory.
func extractTar(tarPath, destDir string) error {
	file, err := os.Open(tarPath)
	if err != nil {
		return err
	}
	defer file.Close()

	tarReader := tar.NewReader(file)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		target := filepath.Join(destDir, header.Name)

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			dir := filepath.Dir(target)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}

			outFile, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR|os.O_TRUNC, header.FileInfo().Mode())
			if err != nil {
				return err
			}

			if _, err := io.Copy(outFile, tarReader); err != nil {
				outFile.Close()
				return err
			}
			outFile.Close()
		}
	}

	return nil
}
