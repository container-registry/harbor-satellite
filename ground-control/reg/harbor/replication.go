package harbor

import (
	"context"
	"fmt"

	v2client "github.com/goharbor/go-client/pkg/sdk/v2.0/client"
	"github.com/goharbor/go-client/pkg/sdk/v2.0/client/replication"
	"github.com/goharbor/go-client/pkg/sdk/v2.0/models"

	_ "github.com/joho/godotenv/autoload"
)

func ListReplication(ctx context.Context, opts ListParams, client *v2client.HarborAPI) ([]*models.ReplicationPolicy, error) {
	response, err := client.Replication.ListReplicationPolicies(
		ctx,
		&replication.ListReplicationPoliciesParams{
			Page:     &opts.Page,
			PageSize: &opts.PageSize,
			Q:        &opts.Q,
			Sort:     &opts.Sort,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("error: listing replication policies: %v", err)
	}
	return response.Payload, nil
}
