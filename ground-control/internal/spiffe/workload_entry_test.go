//go:build !nospiffe

package spiffe

import (
	"context"
	"fmt"
	"testing"

	"github.com/spiffe/go-spiffe/v2/spiffeid"
	entryv1 "github.com/spiffe/spire-api-sdk/proto/spire/api/server/entry/v1"
	typesv1 "github.com/spiffe/spire-api-sdk/proto/spire/api/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

// mockEntryClient implements entryv1.EntryClient for testing.
type mockEntryClient struct {
	entryv1.EntryClient

	batchCreateResp *entryv1.BatchCreateEntryResponse
	batchCreateErr  error

	batchDeleteResp *entryv1.BatchDeleteEntryResponse
	batchDeleteErr  error

	// Track calls for assertion
	createCalled bool
	deleteCalled bool
	deleteIDs    []string
}

func (m *mockEntryClient) BatchCreateEntry(_ context.Context, _ *entryv1.BatchCreateEntryRequest, _ ...grpc.CallOption) (*entryv1.BatchCreateEntryResponse, error) {
	m.createCalled = true
	return m.batchCreateResp, m.batchCreateErr
}

func (m *mockEntryClient) BatchDeleteEntry(_ context.Context, req *entryv1.BatchDeleteEntryRequest, _ ...grpc.CallOption) (*entryv1.BatchDeleteEntryResponse, error) {
	m.deleteCalled = true
	m.deleteIDs = req.Ids
	return m.batchDeleteResp, m.batchDeleteErr
}

func newTestClient(entry *mockEntryClient) *ServerClient {
	td, _ := spiffeid.TrustDomainFromString("example.com")
	return &ServerClient{
		entryClient: entry,
		trustDomain: td,
	}
}

func TestCreateWorkloadEntry(t *testing.T) {
	tests := []struct {
		name      string
		mock      *mockEntryClient
		wantID    string
		wantErr   string
		selectors []string
	}{
		{
			name: "success",
			mock: &mockEntryClient{
				batchCreateResp: &entryv1.BatchCreateEntryResponse{
					Results: []*entryv1.BatchCreateEntryResponse_Result{
						{
							Status: &typesv1.Status{Code: 0},
							Entry:  &typesv1.Entry{Id: "entry-123"},
						},
					},
				},
			},
			selectors: []string{"unix:uid:1000"},
			wantID:    "entry-123",
		},
		{
			name: "gRPC error",
			mock: &mockEntryClient{
				batchCreateErr: fmt.Errorf("connection refused"),
			},
			selectors: []string{"unix:uid:1000"},
			wantErr:   "create workload entry: connection refused",
		},
		{
			name: "empty response",
			mock: &mockEntryClient{
				batchCreateResp: &entryv1.BatchCreateEntryResponse{},
			},
			selectors: []string{"unix:uid:1000"},
			wantErr:   "create workload entry: empty response",
		},
		{
			name: "nil entry in result",
			mock: &mockEntryClient{
				batchCreateResp: &entryv1.BatchCreateEntryResponse{
					Results: []*entryv1.BatchCreateEntryResponse_Result{
						{Status: &typesv1.Status{Code: 2, Message: "internal error"}},
					},
				},
			},
			selectors: []string{"unix:uid:1000"},
			wantErr:   "create workload entry: empty response",
		},
		{
			name: "already exists status rejected",
			mock: &mockEntryClient{
				batchCreateResp: &entryv1.BatchCreateEntryResponse{
					Results: []*entryv1.BatchCreateEntryResponse_Result{
						{
							Status: &typesv1.Status{Code: 6, Message: "entry already exists"},
							Entry:  &typesv1.Entry{Id: "existing-entry-456"},
						},
					},
				},
			},
			selectors: []string{"unix:uid:1000"},
			wantErr:   "create workload entry: status 6",
		},
		{
			name: "non-zero status without entry",
			mock: &mockEntryClient{
				batchCreateResp: &entryv1.BatchCreateEntryResponse{
					Results: []*entryv1.BatchCreateEntryResponse_Result{
						{
							Status: &typesv1.Status{Code: 3, Message: "invalid argument"},
						},
					},
				},
			},
			selectors: []string{"unix:uid:1000"},
			wantErr:   "create workload entry: empty response",
		},
		{
			name:      "invalid selector format",
			mock:      &mockEntryClient{},
			selectors: []string{"no-colon"},
			wantErr:   "invalid selector format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newTestClient(tt.mock)
			id, err := client.CreateWorkloadEntry(
				context.Background(),
				"spiffe://example.com/agent/sat-01",
				"spiffe://example.com/satellite/region/default/sat-01",
				tt.selectors,
			)

			if tt.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
				require.Empty(t, id)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.wantID, id)
			}
		})
	}
}

func TestDeleteWorkloadEntry(t *testing.T) {
	tests := []struct {
		name    string
		mock    *mockEntryClient
		entryID string
		wantErr string
	}{
		{
			name: "success",
			mock: &mockEntryClient{
				batchDeleteResp: &entryv1.BatchDeleteEntryResponse{},
			},
			entryID: "entry-123",
		},
		{
			name: "gRPC error",
			mock: &mockEntryClient{
				batchDeleteErr: fmt.Errorf("connection refused"),
			},
			entryID: "entry-123",
			wantErr: "delete workload entry entry-123: connection refused",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newTestClient(tt.mock)
			err := client.DeleteWorkloadEntry(context.Background(), tt.entryID)

			if tt.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
			} else {
				require.NoError(t, err)
				require.True(t, tt.mock.deleteCalled)
				require.Equal(t, []string{tt.entryID}, tt.mock.deleteIDs)
			}
		})
	}
}

func TestCreateWorkloadEntry_AlreadyExistsNotReturned(t *testing.T) {
	mock := &mockEntryClient{
		batchCreateResp: &entryv1.BatchCreateEntryResponse{
			Results: []*entryv1.BatchCreateEntryResponse_Result{
				{
					Status: &typesv1.Status{Code: 6, Message: "already exists"},
					Entry:  &typesv1.Entry{Id: "someone-elses-entry"},
				},
			},
		},
	}

	client := newTestClient(mock)
	id, err := client.CreateWorkloadEntry(
		context.Background(),
		"spiffe://example.com/agent/sat-01",
		"spiffe://example.com/satellite/region/default/sat-01",
		[]string{"unix:uid:1000"},
	)

	// Must NOT return the pre-existing entry ID
	require.Error(t, err)
	require.Empty(t, id)
	require.Contains(t, err.Error(), "status 6")
}
