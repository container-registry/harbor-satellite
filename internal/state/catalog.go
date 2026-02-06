package state

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

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

func collectCachedImages(ctx context.Context, registryURL string, insecure bool) ([]CachedImage, error) {
	log := logger.FromContext(ctx)

	repos, err := fetchCatalog(ctx, registryURL)
	if err != nil {
		return nil, fmt.Errorf("fetch catalog: %w", err)
	}

	if len(repos) == 0 {
		return []CachedImage{}, nil
	}

	var images []CachedImage
	for _, repo := range repos {
		tags, err := fetchTags(ctx, registryURL, repo)
		if err != nil {
			log.Warn().Err(err).Str("repo", repo).Msg("Skipping repo: failed to fetch tags")
			continue
		}
		for _, tag := range tags {
			ref := fmt.Sprintf("%s/%s:%s", registryURL, repo, tag)
			img, err := collectImageInfo(ctx, ref, insecure)
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

func collectImageInfo(ctx context.Context, ref string, insecure bool) (CachedImage, error) {
	opts := []crane.Option{crane.WithContext(ctx)}
	if insecure {
		opts = append(opts, crane.Insecure)
	}

	digest, err := crane.Digest(ref, opts...)
	if err != nil {
		return CachedImage{}, fmt.Errorf("get digest for %s: %w", ref, err)
	}

	manifest, err := crane.Manifest(ref, opts...)
	if err != nil {
		return CachedImage{}, fmt.Errorf("get manifest for %s: %w", ref, err)
	}

	size, err := computeManifestSize(manifest)
	if err != nil {
		return CachedImage{}, fmt.Errorf("compute size for %s: %w", ref, err)
	}

	return CachedImage{
		Reference: ref + "@" + digest,
		SizeBytes: size,
	}, nil
}

func computeManifestSize(raw []byte) (int64, error) {
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

func fetchCatalog(ctx context.Context, registryURL string) ([]string, error) {
	url := fmt.Sprintf("http://%s/v2/_catalog", registryURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create catalog request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send catalog request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("catalog request returned %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read catalog response: %w", err)
	}

	var catalog catalogResponse
	if err := json.Unmarshal(body, &catalog); err != nil {
		return nil, fmt.Errorf("unmarshal catalog: %w", err)
	}
	return catalog.Repositories, nil
}

func fetchTags(ctx context.Context, registryURL, repo string) ([]string, error) {
	url := fmt.Sprintf("http://%s/v2/%s/tags/list", registryURL, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create tags request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send tags request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tags request returned %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read tags response: %w", err)
	}

	var tags tagsResponse
	if err := json.Unmarshal(body, &tags); err != nil {
		return nil, fmt.Errorf("unmarshal tags: %w", err)
	}
	return tags.Tags, nil
}
