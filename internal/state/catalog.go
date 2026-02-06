package state

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/go-containerregistry/pkg/crane"
	v1 "github.com/google/go-containerregistry/pkg/v1"

	"github.com/container-registry/harbor-satellite/internal/logger"
)

type CachedImage struct {
	Reference string `json:"reference"`
	SizeBytes int64  `json:"size_bytes"`
}

type catalogResponse struct {
	Repositories []string `json:"repositories"`
}

type tagsResponse struct {
	Tags []string `json:"tags"`
}

func collectCachedImages(ctx context.Context, registryHost string, insecure bool) ([]CachedImage, error) {
	log := logger.FromContext(ctx)
	client := &http.Client{Timeout: 30 * time.Second}

	repos, err := fetchCatalog(ctx, client, registryHost, insecure)
	if err != nil {
		return nil, fmt.Errorf("fetch catalog: %w", err)
	}

	var images []CachedImage
	for _, repo := range repos {
		tags, err := fetchTags(ctx, client, registryHost, repo, insecure)
		if err != nil {
			log.Warn().Err(err).Str("repo", repo).Msg("Skipping repo: failed to fetch tags")
			continue
		}
		for _, tag := range tags {
			ref := fmt.Sprintf("%s/%s:%s", registryHost, repo, tag)
			img, err := collectImageInfo(ref, crane.WithContext(ctx), insecure)
			if err != nil {
				log.Warn().Err(err).Str("ref", ref).Msg("Skipping image: failed to collect info")
				continue
			}
			images = append(images, img)
		}
	}

	if images == nil {
		return []CachedImage{}, nil
	}
	return images, nil
}

func collectImageInfo(ref string, ctxOpt crane.Option, insecure bool) (CachedImage, error) {
	opts := []crane.Option{ctxOpt}
	if insecure {
		opts = append(opts, crane.Insecure)
	}

	raw, err := crane.Manifest(ref, opts...)
	if err != nil {
		return CachedImage{}, fmt.Errorf("get manifest for %s: %w", ref, err)
	}

	size, err := computeManifestSize(raw)
	if err != nil {
		return CachedImage{}, fmt.Errorf("compute size for %s: %w", ref, err)
	}

	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(raw))

	return CachedImage{
		Reference: ref + "@" + digest,
		SizeBytes: size,
	}, nil
}

func computeManifestSize(raw []byte) (int64, error) {
	var probe struct {
		MediaType string `json:"mediaType"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return 0, fmt.Errorf("unmarshal manifest: %w", err)
	}

	switch probe.MediaType {
	case "application/vnd.oci.image.index.v1+json",
		"application/vnd.docker.distribution.manifest.list.v2+json":
		return 0, fmt.Errorf("multi-arch manifest index not supported for size computation")
	}

	var m v1.Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return 0, fmt.Errorf("unmarshal manifest: %w", err)
	}

	var total int64
	total += m.Config.Size
	for _, layer := range m.Layers {
		total += layer.Size
	}
	return total, nil
}

func registryScheme(insecure bool) string {
	if insecure {
		return "http"
	}
	return "https"
}

func fetchCatalog(ctx context.Context, client *http.Client, registryHost string, insecure bool) ([]string, error) {
	url := fmt.Sprintf("%s://%s/v2/_catalog", registryScheme(insecure), registryHost)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create catalog request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("catalog request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("catalog request returned %s", resp.Status)
	}

	var catalog catalogResponse
	if err := json.NewDecoder(resp.Body).Decode(&catalog); err != nil {
		return nil, fmt.Errorf("decode catalog: %w", err)
	}
	return catalog.Repositories, nil
}

func fetchTags(ctx context.Context, client *http.Client, registryHost, repo string, insecure bool) ([]string, error) {
	url := fmt.Sprintf("%s://%s/v2/%s/tags/list", registryScheme(insecure), registryHost, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create tags request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tags request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tags request returned %s", resp.Status)
	}

	var tags tagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return nil, fmt.Errorf("decode tags: %w", err)
	}
	return tags.Tags, nil
}
