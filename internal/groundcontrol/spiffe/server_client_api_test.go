//go:build !nospiffe

package spiffe

import (
	"context"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/spiffe/spiretest"
	typesv1 "github.com/spiffe/spire-api-sdk/proto/spire/api/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

const td = "example.org"

func newAPIClient(t *testing.T) (*ServerClient, *spiretest.Server) {
	t.Helper()
	srv := spiretest.Start(t)
	c, err := NewServerClient("unix://"+srv.Socket, td)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, c.Close()) })
	return c, srv
}

func id(path string) *typesv1.SPIFFEID { return &typesv1.SPIFFEID{TrustDomain: td, Path: path} }

func TestNewServerClientRejectsABadTrustDomain(t *testing.T) {
	_, err := NewServerClient("/tmp/none.sock", "Not A Domain")
	require.ErrorContains(t, err, "invalid trust domain")
}

func TestServerClientTrustDomain(t *testing.T) {
	c, _ := newAPIClient(t)
	require.Equal(t, td, c.TrustDomainName())
	require.Equal(t, td, c.GetTrustDomain().String())
}

func TestCreateJoinTokenSendsTheAgentPathAndTTL(t *testing.T) {
	c, srv := newAPIClient(t)
	ctx := context.Background()

	token, err := c.CreateJoinToken(ctx, "spiffe://"+td+"/agent/edge-01", 10*time.Minute)
	require.NoError(t, err)
	require.Equal(t, "join-1", token)
	_, err = c.CreateJoinToken(ctx, "agent/edge-02", time.Hour)
	require.NoError(t, err)

	reqs := srv.JoinTokenRequests()
	require.Len(t, reqs, 2)
	require.Equal(t, int32(600), reqs[0].GetTtl())
	require.Equal(t, "/agent/edge-01", reqs[0].GetAgentId().GetPath())
	require.Equal(t, td, reqs[0].GetAgentId().GetTrustDomain())
	require.Equal(t, "/agent/edge-02", reqs[1].GetAgentId().GetPath())
}

func TestCreateWorkloadEntryStoresParentSPIFFEIDAndSelectors(t *testing.T) {
	c, srv := newAPIClient(t)

	entryID, err := c.CreateWorkloadEntry(context.Background(),
		"spiffe://"+td+"/agent/edge-01", "spiffe://"+td+"/satellite/region/eu/edge-01",
		[]string{"unix:uid:0", "docker:label:app:satellite"})
	require.NoError(t, err)

	e := srv.Entries()[entryID]
	require.NotNil(t, e)
	require.Equal(t, "/agent/edge-01", e.GetParentId().GetPath())
	require.Equal(t, "/satellite/region/eu/edge-01", e.GetSpiffeId().GetPath())
	require.Len(t, e.GetSelectors(), 2)
	require.Equal(t, "unix", e.GetSelectors()[0].GetType())
	require.Equal(t, "uid:0", e.GetSelectors()[0].GetValue())
	require.Equal(t, "label:app:satellite", e.GetSelectors()[1].GetValue(), "only the first colon splits type from value")
}

func TestCreateWorkloadEntryErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("selector without a type", func(t *testing.T) {
		c, srv := newAPIClient(t)
		_, err := c.CreateWorkloadEntry(ctx, "/agent/a", "/sat/a", []string{"uid0"})
		require.ErrorContains(t, err, `invalid selector format "uid0"`)
		require.Empty(t, srv.Entries(), "nothing is sent for an invalid selector")
	})
	t.Run("per-entry status", func(t *testing.T) {
		c, srv := newAPIClient(t)
		srv.FailCreates(codes.AlreadyExists)
		_, err := c.CreateWorkloadEntry(ctx, "/agent/a", "/sat/a", []string{"unix:uid:0"})
		require.ErrorContains(t, err, "status 6 - create refused", "SPIRE's reason reaches the caller")
	})
	t.Run("rpc error", func(t *testing.T) {
		c, srv := newAPIClient(t)
		srv.FailRPCs(grpcstatus.Error(codes.Unavailable, "down"))
		_, err := c.CreateWorkloadEntry(ctx, "/agent/a", "/sat/a", nil)
		require.ErrorContains(t, err, "create workload entry")
		require.Equal(t, codes.Unavailable, grpcstatus.Code(err))
	})
}

func TestListAgentsFollowsPagesAndFilters(t *testing.T) {
	c, srv := newAPIClient(t)
	srv.SetPageSize(2)
	exp := time.Now().Add(time.Hour).Truncate(time.Second)
	for i, typ := range []string{"x509pop", "join_token", "x509pop", "x509pop", "sshpop"} {
		srv.AddAgent(&typesv1.Agent{
			Id:                id("/agent/" + string(rune('a'+i))),
			AttestationType:   typ,
			X509SvidExpiresAt: exp.Unix(),
			Selectors:         []*typesv1.Selector{{Type: "x509pop", Value: "subject:cn:" + string(rune('a'+i))}},
		})
	}
	srv.AddAgent(&typesv1.Agent{AttestationType: "x509pop"})

	agents, err := c.ListAgents(context.Background(), "x509pop")
	require.NoError(t, err)
	require.Len(t, agents, 3, "an agent without an ID is skipped")
	require.Equal(t, "spiffe://"+td+"/agent/a", agents[0].SpiffeID)
	require.Equal(t, "spiffe://"+td+"/agent/d", agents[2].SpiffeID)
	require.Equal(t, []string{"x509pop:subject:cn:a"}, agents[0].Selectors)
	require.True(t, exp.Equal(agents[0].ExpiresAt))

	all, err := c.ListAgents(context.Background(), "")
	require.NoError(t, err)
	require.Len(t, all, 5)
}

func TestListAgentsRPCError(t *testing.T) {
	c, srv := newAPIClient(t)
	srv.FailRPCs(grpcstatus.Error(codes.PermissionDenied, "no"))
	_, err := c.ListAgents(context.Background(), "")
	require.ErrorContains(t, err, "list agents")
}

func TestDeleteEntriesBySPIFFEIDDeletesOnlyThatID(t *testing.T) {
	c, srv := newAPIClient(t)
	srv.SetPageSize(1)
	mine := id("/satellite/region/eu/edge-01")
	srv.AddEntry(&typesv1.Entry{SpiffeId: mine, ParentId: id("/agent/a")})
	srv.AddEntry(&typesv1.Entry{SpiffeId: mine, ParentId: id("/agent/b")})
	other := srv.AddEntry(&typesv1.Entry{SpiffeId: id("/satellite/region/eu/edge-02"), ParentId: id("/agent/a")})

	require.NoError(t, c.DeleteEntriesBySPIFFEID(context.Background(), "spiffe://"+td+"/satellite/region/eu/edge-01"))

	left := srv.Entries()
	require.Len(t, left, 1)
	require.Contains(t, left, other)

	require.NoError(t, c.DeleteEntriesBySPIFFEID(context.Background(), "spiffe://"+td+"/satellite/region/eu/edge-01"),
		"no entries left is not an error")
}

func TestDeleteWorkloadEntryOverTheAPI(t *testing.T) {
	c, srv := newAPIClient(t)
	e := srv.AddEntry(&typesv1.Entry{SpiffeId: id("/s"), ParentId: id("/a")})
	require.NoError(t, c.DeleteWorkloadEntry(context.Background(), e))
	require.Empty(t, srv.Entries())
}

func TestDeleteRPCErrors(t *testing.T) {
	c, srv := newAPIClient(t)
	srv.AddEntry(&typesv1.Entry{SpiffeId: id("/s"), ParentId: id("/a")})
	srv.FailRPCs(grpcstatus.Error(codes.Unavailable, "down"))
	require.ErrorContains(t, c.DeleteWorkloadEntry(context.Background(), "entry-1"), "delete workload entry entry-1")
	require.ErrorContains(t, c.DeleteEntriesBySPIFFEID(context.Background(), "/s"), "list entries of /s")
}

// SPIRE answers a batch delete with OK and puts the outcome of each entry
// in its result; a refused delete must not look like success.
func TestDeletesReportAPerEntryFailure(t *testing.T) {
	ctx := context.Background()

	t.Run("by entry ID", func(t *testing.T) {
		c, srv := newAPIClient(t)
		e := srv.AddEntry(&typesv1.Entry{SpiffeId: id("/s"), ParentId: id("/a")})
		srv.FailDelete(e, codes.Internal)
		require.ErrorContains(t, c.DeleteWorkloadEntry(ctx, e), "delete refused")
		require.Len(t, srv.Entries(), 1)
	})
	t.Run("by SPIFFE ID", func(t *testing.T) {
		c, srv := newAPIClient(t)
		srv.AddEntry(&typesv1.Entry{SpiffeId: id("/s"), ParentId: id("/a")})
		kept := srv.AddEntry(&typesv1.Entry{SpiffeId: id("/s"), ParentId: id("/b")})
		srv.FailDelete(kept, codes.PermissionDenied)
		err := c.DeleteEntriesBySPIFFEID(ctx, "spiffe://"+td+"/s")
		require.ErrorContains(t, err, kept)
		require.ErrorContains(t, err, "delete refused")
		require.Len(t, srv.Entries(), 1)
	})
	t.Run("an entry already gone is deleted", func(t *testing.T) {
		c, _ := newAPIClient(t)
		require.NoError(t, c.DeleteWorkloadEntry(ctx, "entry-404"))
	})
}
