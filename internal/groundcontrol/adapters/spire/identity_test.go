//go:build !nospiffe

package spire

import (
	"context"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/spiffe"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/spiffe/spiretest"
	typesv1 "github.com/spiffe/spire-api-sdk/proto/spire/api/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const td = "edge.example"

func newIdentity(t *testing.T) (*WorkloadIdentity, *spiretest.Server) {
	t.Helper()
	srv := spiretest.Start(t)
	client, err := spiffe.NewServerClient(srv.Socket, td)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	w, err := New(client)
	require.NoError(t, err)
	return w, srv
}

func TestNewNeedsAClient(t *testing.T) {
	_, err := New(nil)
	require.ErrorIs(t, err, fleet.ErrInvalid)
}

// The registration flow in fleet: a join token for the agent, a workload
// entry under it, and on satellite deletion every entry of its SPIFFE ID.
func TestRegistrationLifecycle(t *testing.T) {
	w, srv := newIdentity(t)
	ctx := context.Background()
	require.Equal(t, td, w.TrustDomain())

	agent := "spiffe://" + td + "/agent/edge-01"
	sat := "spiffe://" + td + "/satellite/region/default/edge-01"
	token, err := w.CreateJoinToken(ctx, agent, 5*time.Minute)
	require.NoError(t, err)
	require.NotEmpty(t, token)
	require.Equal(t, "/agent/edge-01", srv.JoinTokenRequests()[0].GetAgentId().GetPath())

	first, err := w.CreateEntry(ctx, agent, sat, []string{"unix:uid:0"})
	require.NoError(t, err)
	_, err = w.CreateEntry(ctx, agent, sat, []string{"unix:uid:1000"})
	require.NoError(t, err)
	other, err := w.CreateEntry(ctx, agent, "spiffe://"+td+"/satellite/region/default/edge-02", []string{"unix:uid:0"})
	require.NoError(t, err)

	require.NoError(t, w.DeleteEntry(ctx, first))
	require.NoError(t, w.DeleteEntriesFor(ctx, sat))
	left := srv.Entries()
	require.Len(t, left, 1)
	require.Contains(t, left, other)
}

func TestListAgentsMapsToFleetAgents(t *testing.T) {
	w, srv := newIdentity(t)
	exp := time.Unix(time.Now().Add(time.Hour).Unix(), 0)
	srv.AddAgent(&typesv1.Agent{
		Id:                &typesv1.SPIFFEID{TrustDomain: td, Path: "/spire/agent/x509pop/abc"},
		AttestationType:   fleet.AttestX509PoP,
		X509SvidExpiresAt: exp.Unix(),
		Selectors:         []*typesv1.Selector{{Type: "x509pop", Value: "subject:cn:edge-01"}},
	})
	srv.AddAgent(&typesv1.Agent{Id: &typesv1.SPIFFEID{TrustDomain: td, Path: "/agent/jt"}, AttestationType: "join_token"})

	agents, err := w.ListAgents(context.Background(), fleet.AttestX509PoP)
	require.NoError(t, err)
	require.Equal(t, []fleet.Agent{{
		SPIFFEID:        "spiffe://" + td + "/spire/agent/x509pop/abc",
		AttestationType: fleet.AttestX509PoP,
		Selectors:       []string{"x509pop:subject:cn:edge-01"},
		ExpiresAt:       exp,
	}}, agents)
}

func TestErrorsPassThrough(t *testing.T) {
	w, srv := newIdentity(t)
	ctx := context.Background()
	srv.FailRPCs(status.Error(codes.Unavailable, "spire down"))

	_, err := w.CreateJoinToken(ctx, "/agent/a", time.Minute)
	require.ErrorContains(t, err, "spire down")
	_, err = w.CreateEntry(ctx, "/agent/a", "/sat/a", nil)
	require.ErrorContains(t, err, "spire down")
	require.ErrorContains(t, w.DeleteEntry(ctx, "entry-1"), "spire down")
	require.ErrorContains(t, w.DeleteEntriesFor(ctx, "/sat/a"), "spire down")
	_, err = w.ListAgents(ctx, "")
	require.ErrorContains(t, err, "spire down")
}
