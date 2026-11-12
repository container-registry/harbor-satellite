// Package spiretest runs an in-memory SPIRE server Agent and Entry API on a
// unix socket, for tests of the Ground Control SPIRE client.
package spiretest

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	agentv1 "github.com/spiffe/spire-api-sdk/proto/spire/api/server/agent/v1"
	entryv1 "github.com/spiffe/spire-api-sdk/proto/spire/api/server/entry/v1"
	typesv1 "github.com/spiffe/spire-api-sdk/proto/spire/api/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/proto"
)

// Server answers like a SPIRE server: batch calls report a per-entry
// status while the RPC itself succeeds.
type Server struct {
	agentv1.UnimplementedAgentServer
	entryv1.UnimplementedEntryServer

	// Socket is the unix socket path the server listens on.
	Socket string

	mu         sync.Mutex
	nextID     int
	entries    map[string]*typesv1.Entry
	agents     []*typesv1.Agent
	joinTokens []*agentv1.CreateJoinTokenRequest
	pageSize   int
	rpcErr     error
	createCode codes.Code
	deleteCode map[string]codes.Code
}

// Start serves on a fresh socket until the test ends.
func Start(t testing.TB) *Server {
	t.Helper()
	// t.TempDir paths on macOS are longer than the 104 bytes a unix socket
	// path may have.
	dir, err := os.MkdirTemp("", "spire")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	s := &Server{
		Socket:     filepath.Join(dir, "api.sock"),
		entries:    map[string]*typesv1.Entry{},
		deleteCode: map[string]codes.Code{},
	}
	lis, err := (&net.ListenConfig{}).Listen(context.Background(), "unix", s.Socket)
	if err != nil {
		t.Fatal(err)
	}
	g := grpc.NewServer()
	agentv1.RegisterAgentServer(g, s)
	entryv1.RegisterEntryServer(g, s)
	served := make(chan error, 1)
	go func() { served <- g.Serve(lis) }()
	t.Cleanup(func() {
		g.Stop()
		if err := <-served; err != nil {
			t.Errorf("SPIRE fake: %v", err)
		}
	})
	return s
}

// SetPageSize makes list calls answer at most n items per page.
func (s *Server) SetPageSize(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pageSize = n
}

// FailRPCs makes every call fail with err at the RPC level.
func (s *Server) FailRPCs(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rpcErr = err
}

// FailCreates makes BatchCreateEntry report code for each entry.
func (s *Server) FailCreates(code codes.Code) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.createCode = code
}

// FailDelete makes BatchDeleteEntry report code for entry id and keep it.
func (s *Server) FailDelete(id string, code codes.Code) {
	s.mu.Lock()
	s.deleteCode[id] = code
	s.mu.Unlock()
}

// AddAgent registers an attested agent.
func (s *Server) AddAgent(a *typesv1.Agent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.agents = append(s.agents, a)
}

// AddEntry stores an entry as if it had been created and returns its ID.
func (s *Server) AddEntry(e *typesv1.Entry) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addLocked(e)
}

func (s *Server) addLocked(e *typesv1.Entry) string {
	s.nextID++
	e = proto.CloneOf(e)
	e.Id = "entry-" + strconv.Itoa(s.nextID)
	s.entries[e.GetId()] = e
	return e.GetId()
}

// Entries returns a copy of the stored entries by ID.
func (s *Server) Entries() map[string]*typesv1.Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]*typesv1.Entry, len(s.entries))
	for id, e := range s.entries {
		out[id] = proto.CloneOf(e)
	}
	return out
}

// JoinTokenRequests returns the CreateJoinToken requests received.
func (s *Server) JoinTokenRequests() []*agentv1.CreateJoinTokenRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*agentv1.CreateJoinTokenRequest(nil), s.joinTokens...)
}

func status(code codes.Code, msg string) *typesv1.Status {
	return &typesv1.Status{Code: int32(code), Message: msg}
}

// page cuts items at the offset in token; the next token is the next offset.
func page[T any](items []T, token string, size int) ([]T, string, error) {
	start := 0
	if token != "" {
		n, err := strconv.Atoi(token)
		if err != nil || n < 0 || n > len(items) {
			return nil, "", fmt.Errorf("bad page token %q", token)
		}
		start = n
	}
	if size <= 0 || start+size >= len(items) {
		return items[start:], "", nil
	}
	return items[start : start+size], strconv.Itoa(start + size), nil
}

func (s *Server) CreateJoinToken(_ context.Context, req *agentv1.CreateJoinTokenRequest) (*typesv1.JoinToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rpcErr != nil {
		return nil, s.rpcErr
	}
	s.joinTokens = append(s.joinTokens, req)
	return &typesv1.JoinToken{Value: fmt.Sprintf("join-%d", len(s.joinTokens))}, nil
}

func (s *Server) ListAgents(_ context.Context, req *agentv1.ListAgentsRequest) (*agentv1.ListAgentsResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rpcErr != nil {
		return nil, s.rpcErr
	}
	var match []*typesv1.Agent
	for _, a := range s.agents {
		if t := req.GetFilter().GetByAttestationType(); t == "" || t == a.GetAttestationType() {
			match = append(match, a)
		}
	}
	agents, next, err := page(match, req.GetPageToken(), s.pageSize)
	if err != nil {
		return nil, err
	}
	return &agentv1.ListAgentsResponse{Agents: agents, NextPageToken: next}, nil
}

func (s *Server) BatchCreateEntry(_ context.Context, req *entryv1.BatchCreateEntryRequest) (*entryv1.BatchCreateEntryResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rpcErr != nil {
		return nil, s.rpcErr
	}
	resp := &entryv1.BatchCreateEntryResponse{}
	for _, e := range req.GetEntries() {
		if s.createCode != codes.OK {
			resp.Results = append(resp.Results, &entryv1.BatchCreateEntryResponse_Result{Status: status(s.createCode, "create refused")})
			continue
		}
		id := s.addLocked(e)
		resp.Results = append(resp.Results, &entryv1.BatchCreateEntryResponse_Result{
			Status: status(codes.OK, "OK"),
			Entry:  proto.CloneOf(s.entries[id]),
		})
	}
	return resp, nil
}

func (s *Server) ListEntries(_ context.Context, req *entryv1.ListEntriesRequest) (*entryv1.ListEntriesResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rpcErr != nil {
		return nil, s.rpcErr
	}
	want := req.GetFilter().GetBySpiffeId()
	var match []*typesv1.Entry
	for i := 1; i <= s.nextID; i++ {
		e, ok := s.entries["entry-"+strconv.Itoa(i)]
		if !ok {
			continue
		}
		if want == nil || proto.Equal(want, e.GetSpiffeId()) {
			match = append(match, proto.CloneOf(e))
		}
	}
	entries, next, err := page(match, req.GetPageToken(), s.pageSize)
	if err != nil {
		return nil, err
	}
	return &entryv1.ListEntriesResponse{Entries: entries, NextPageToken: next}, nil
}

func (s *Server) BatchDeleteEntry(_ context.Context, req *entryv1.BatchDeleteEntryRequest) (*entryv1.BatchDeleteEntryResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rpcErr != nil {
		return nil, s.rpcErr
	}
	resp := &entryv1.BatchDeleteEntryResponse{}
	for _, id := range req.GetIds() {
		st := status(codes.OK, "OK")
		switch code, failed := s.deleteCode[id]; {
		case failed:
			st = status(code, "delete refused")
		case s.entries[id] == nil:
			st = status(codes.NotFound, "entry not found")
		default:
			delete(s.entries, id)
		}
		resp.Results = append(resp.Results, &entryv1.BatchDeleteEntryResponse_Result{Status: st, Id: id})
	}
	return resp, nil
}
