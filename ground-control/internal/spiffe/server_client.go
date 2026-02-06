//go:build !nospiffe

package spiffe

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/spiffe/go-spiffe/v2/spiffeid"
	agentv1 "github.com/spiffe/spire-api-sdk/proto/spire/api/server/agent/v1"
	entryv1 "github.com/spiffe/spire-api-sdk/proto/spire/api/server/entry/v1"
	typesv1 "github.com/spiffe/spire-api-sdk/proto/spire/api/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// ServerClient provides access to SPIRE Server APIs via gRPC.
type ServerClient struct {
	conn        *grpc.ClientConn
	agentClient agentv1.AgentClient
	entryClient entryv1.EntryClient
	trustDomain spiffeid.TrustDomain
}

// NewServerClient creates a new client connected to the SPIRE Server socket.
func NewServerClient(socketPath, trustDomain string) (*ServerClient, error) {
	td, err := spiffeid.TrustDomainFromString(trustDomain)
	if err != nil {
		return nil, fmt.Errorf("invalid trust domain %q: %w", trustDomain, err)
	}

	// Clean socket path (remove unix:// prefix if present)
	cleanPath := strings.TrimPrefix(socketPath, "unix://")

	conn, err := grpc.NewClient(
		"unix://"+cleanPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return net.DialTimeout("unix", cleanPath, 5*time.Second)
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("connect to SPIRE server at %s: %w", socketPath, err)
	}

	return &ServerClient{
		conn:        conn,
		agentClient: agentv1.NewAgentClient(conn),
		entryClient: entryv1.NewEntryClient(conn),
		trustDomain: td,
	}, nil
}

// CreateJoinToken creates a join token for agent attestation.
// The token can be used by a SPIRE agent to bootstrap with the server.
func (c *ServerClient) CreateJoinToken(ctx context.Context, spiffeID string, ttl time.Duration) (string, error) {
	path := extractPath(spiffeID, c.trustDomain.String())

	resp, err := c.agentClient.CreateJoinToken(ctx, &agentv1.CreateJoinTokenRequest{
		Ttl: int32(ttl.Seconds()),
		AgentId: &typesv1.SPIFFEID{
			TrustDomain: c.trustDomain.String(),
			Path:        path,
		},
	})
	if err != nil {
		return "", fmt.Errorf("create join token: %w", err)
	}

	return resp.Value, nil
}

// CreateWorkloadEntry creates a registration entry for a workload.
// The entry associates a SPIFFE ID with selectors that identify the workload.
// Returns the entry ID for cleanup on failure.
func (c *ServerClient) CreateWorkloadEntry(ctx context.Context, parentID, spiffeID string, selectors []string) (string, error) {
	parentPath := extractPath(parentID, c.trustDomain.String())
	workloadPath := extractPath(spiffeID, c.trustDomain.String())

	var selectorList []*typesv1.Selector
	for _, sel := range selectors {
		parts := strings.SplitN(sel, ":", 2)
		if len(parts) != 2 {
			return "", fmt.Errorf("invalid selector format %q, expected type:value", sel)
		}
		selectorList = append(selectorList, &typesv1.Selector{
			Type:  parts[0],
			Value: parts[1],
		})
	}

	resp, err := c.entryClient.BatchCreateEntry(ctx, &entryv1.BatchCreateEntryRequest{
		Entries: []*typesv1.Entry{
			{
				ParentId: &typesv1.SPIFFEID{
					TrustDomain: c.trustDomain.String(),
					Path:        parentPath,
				},
				SpiffeId: &typesv1.SPIFFEID{
					TrustDomain: c.trustDomain.String(),
					Path:        workloadPath,
				},
				Selectors: selectorList,
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("create workload entry: %w", err)
	}

	if len(resp.Results) == 0 || resp.Results[0].Entry == nil {
		return "", fmt.Errorf("create workload entry: empty response")
	}

	return resp.Results[0].Entry.Id, nil
}

// DeleteWorkloadEntry removes a workload registration entry by ID.
func (c *ServerClient) DeleteWorkloadEntry(ctx context.Context, entryID string) error {
	_, err := c.entryClient.BatchDeleteEntry(ctx, &entryv1.BatchDeleteEntryRequest{
		Ids: []string{entryID},
	})
	if err != nil {
		return fmt.Errorf("delete workload entry %s: %w", entryID, err)
	}
	return nil
}

// GetTrustDomain returns the configured trust domain.
func (c *ServerClient) GetTrustDomain() spiffeid.TrustDomain {
	return c.trustDomain
}

// Close closes the gRPC connection.
func (c *ServerClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// AgentInfo contains information about an attested SPIRE agent.
type AgentInfo struct {
	SpiffeID        string
	AttestationType string
	Selectors       []string
	ExpiresAt       time.Time
}

// ListAgents lists attested agents, optionally filtered by attestation type.
func (c *ServerClient) ListAgents(ctx context.Context, attestationType string) ([]AgentInfo, error) {
	var agents []AgentInfo
	var pageToken string

	for {
		req := &agentv1.ListAgentsRequest{
			PageToken: pageToken,
		}

		if attestationType != "" {
			req.Filter = &agentv1.ListAgentsRequest_Filter{
				ByAttestationType: attestationType,
			}
		}

		resp, err := c.agentClient.ListAgents(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("list agents: %w", err)
		}

		for _, agent := range resp.Agents {
			if agent.Id == nil {
				continue
			}
			info := AgentInfo{
				SpiffeID:        fmt.Sprintf("spiffe://%s%s", agent.Id.TrustDomain, agent.Id.Path),
				AttestationType: agent.AttestationType,
			}

			if agent.X509SvidExpiresAt > 0 {
				info.ExpiresAt = time.Unix(agent.X509SvidExpiresAt, 0)
			}

			for _, sel := range agent.Selectors {
				info.Selectors = append(info.Selectors, fmt.Sprintf("%s:%s", sel.Type, sel.Value))
			}

			agents = append(agents, info)
		}

		pageToken = resp.NextPageToken
		if pageToken == "" {
			break
		}
	}

	return agents, nil
}

// extractPath extracts the path component from a SPIFFE ID.
// Input: "spiffe://domain/path/to/workload" -> "/path/to/workload"
func extractPath(spiffeID, trustDomain string) string {
	prefix := fmt.Sprintf("spiffe://%s", trustDomain)
	if strings.HasPrefix(spiffeID, prefix) {
		return strings.TrimPrefix(spiffeID, prefix)
	}
	// If it's already just a path, return as-is
	if strings.HasPrefix(spiffeID, "/") {
		return spiffeID
	}
	return "/" + spiffeID
}
