package reg

import (
	"context"
	"fmt"
	"log"
	"os"

	"gopkg.in/yaml.v2"

	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	oras "oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/file"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"
)

type SatelliteState struct {
	Group    string   `json:"group"            yaml:"group"`
	Registry string   `json:"registry"         yaml:"registry"`
	Images   []Images `json:"images,omitempty" yaml:"images,omitempty"`
}

// TO-DO: Auth for specific images experimental
// type Auth struct {
// 	Username string `json:"username" yaml:"username"`
// 	Password string `json:"password" yaml:"password"`
// }

type Images struct {
	Registry   string `json:"registry"         yaml:"registry"`
	Repository string `json:"repository"       yaml:"repository"`
	Tag        string `json:"tag"              yaml:"tag"`
	Digest     string `json:"digest,omitempty" yaml:"digest,omitempty"`

	// Auth       Auth `json:"auth,omitempty" yaml:"auth,omitempty"`
}

// Creates State artifact & push to registry.
func PushStateArtifact(State SatelliteState, ctx context.Context) {
	// generate the yaml content from state
	yamlData, err := yaml.Marshal(State)
	if err != nil {
		fmt.Printf("Error while Marshaling. %v", err)
	}

	fmt.Println(" --- YAML (Start) ---")
	fmt.Println(string(yamlData))
	fmt.Println(" --- YAML (END) ---")

	// Create the file in /tmp/
	tmpFile, err := os.CreateTemp("/tmp/", "state-*.yaml")
	if err != nil {
		panic(err)
	}
	defer os.Remove(tmpFile.Name()) // Clean up the file after execution

	if _, err := tmpFile.Write(yamlData); err != nil {
		panic(err)
	}
	if err := tmpFile.Close(); err != nil {
		panic(err)
	}

	// Create a file store
	fs, err := file.New("/tmp/")
	if err != nil {
		panic(err)
	}
	defer fs.Close()

	// Add files to the file store
	mediaType := "application/vnd.test.file"
	fileNames := []string{fmt.Sprintf("%s", tmpFile.Name())}
	fileDescriptors := make([]v1.Descriptor, 0, len(fileNames))
	for _, name := range fileNames {
		fileDescriptor, err := fs.Add(ctx, name, mediaType, "")
		if err != nil {
			panic(err)
		}
		fileDescriptors = append(fileDescriptors, fileDescriptor)
		fmt.Printf("file descriptor for %s: %v\n", name, fileDescriptor)
	}

	// Pack the files and tag the packed manifest
	artifactType := "application/vnd.test.artifact"
	opts := oras.PackManifestOptions{
		Layers: fileDescriptors,
	}
	manifestDescriptor, err := oras.PackManifest(
		ctx,
		fs,
		oras.PackManifestVersion1_1,
		artifactType,
		opts,
	)
	if err != nil {
		panic(err)
	}
	fmt.Println("manifest descriptor:", manifestDescriptor)

	tag := "latest"
	if err = fs.Tag(ctx, manifestDescriptor, tag); err != nil {
		panic(err)
	}

	// Connect to a remote repository
	repo, err := remote.NewRepository(fmt.Sprintf("%s/satellite/%s", State.Registry, State.Group))
	if err != nil {
		panic(err)
	}

	repo.Client = &auth.Client{
		Client: retry.DefaultClient,
		Cache:  auth.NewCache(),
		Credential: auth.StaticCredential(State.Registry, auth.Credential{
			// Username: "admin",
			// Password: "Harbor12345",
			Username: os.Getenv("HARBOR_USERNAME"),
			Password: os.Getenv("HARBOR_PASSWORD"),
		}),
	}

	_, err = repo.Exists(ctx, manifestDescriptor)
	if err != nil {
		fmt.Println("error checking with existing repo")
		log.Fatalf(
			"please create project named 'satellite' for storing satellite state artifact: %v",
			err,
		)
	}

	// Copy State to remote repository
	_, err = oras.Copy(ctx, fs, tag, repo, tag, oras.DefaultCopyOptions)
	if err != nil {
		panic(err)
	}
}
