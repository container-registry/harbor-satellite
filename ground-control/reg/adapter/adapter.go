package main

import (
	"context"
	"fmt"
	"log"

	"github.com/goharbor/harbor/src/pkg/reg"
	"github.com/goharbor/harbor/src/pkg/reg/adapter"
	"github.com/goharbor/harbor/src/pkg/reg/model"
)

func main() {
	ctx := context.Background()
	fmt.Println("adapter running...")

	HarborList(ctx)
}

func HarborList(ctx context.Context) {
	// create new registry adapter manager
	mgr := reg.NewManager()

	types, err := mgr.ListRegistryProviderTypes(ctx)
	if err != nil {
		log.Printf("Error in tyupe: %v", err)
	}
	fmt.Println("reg tyeps:", types)

	reg := &model.Registry{
		Name:     "dockerReg",
		Type:     model.RegistryTypeHarbor,
		URL:      "https://demo.goharbor.io",
		Insecure: false,
		Status:   "inactive",
		Credential: &model.Credential{
      Type: model.CredentialTypeBasic,
      AccessKey:    "robot$kumar",
			AccessSecret: "Harbor12345",
		},
	}

	factry, err := adapter.GetFactory(model.RegistryTypeDockerRegistry)
	if err != nil {
		log.Printf("Error in getting Factory: %v", err)
	}

	fmt.Println("factory cratead: ", factry)

	// do some ops
	fadapter, err := factry.Create(reg)
	if err != nil {
		log.Printf("Error in getting Factory adapter: %v", err)
	}
	fmt.Println("factory adapter cratead: ", fadapter)

	// Type assert the adapter to ArtifactRegistry
	if artifactRegistry, ok := fadapter.(adapter.ArtifactRegistry); !ok {
		log.Println("The adapter does not implement ArtifactRegistry interface")
	} else {
		artifacts, err := artifactRegistry.FetchArtifacts(nil)
		if err != nil {
			log.Printf("Error in fetching artifacts: %v", err)
		} else {
			fmt.Println("Fetched artifacts:", artifacts)
		}

		if artifacts != nil {
			for _, artifact := range artifacts {
				if artifact != nil {
					// fmt.Printf("Fetched artifact: %v\n", artifact)
				} else {
					// fmt.Println("Fetched a nil artifact")
				}
			}
		} else {
			fmt.Println("No artifacts fetched")
		}
	}
}
