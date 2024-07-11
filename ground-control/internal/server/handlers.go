package server

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"container-registry.com/harbor-satellite/ground-control/internal/database"
)

type CreateGroupRequest struct {
	GroupName string `json:"group_name"`
	Username  string `json:"username"`
	Password  string `json:"password"`
}

func (s *Server) HelloWorldHandler(w http.ResponseWriter, r *http.Request) {
	resp := make(map[string]string)
	resp["message"] = "Hello World"

	jsonResp, err := json.Marshal(resp)
	if err != nil {
		log.Fatalf("error handling JSON marshal. Err: %v", err)
	}

	_, _ = w.Write(jsonResp)
}

func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	err := s.db.Ping()
	if err != nil {
		log.Fatalf("error pinging db: %v", err)
	}

	jsonResp, err := json.Marshal(map[string]string{"status": "healthy"})
	if err != nil {
		log.Fatalf("error handling JSON marshal. Err: %v", err)
	}

	_, _ = w.Write(jsonResp)
}

func (s *Server) createGroupHandler(w http.ResponseWriter, r *http.Request) {
	// Decode request body
	var req CreateGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	params := database.CreateGroupParams{
		GroupName: req.GroupName,
		Username:  req.Username,
		Password:  req.Password,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// Call the database query
	result, err := s.dbQueries.CreateGroup(context.Background(), params)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	jsonResp, err := json.Marshal(result)
	if err != nil {
		log.Fatalf("error handling JSON marshal. Err: %v", err)
	}

	_, _ = w.Write(jsonResp)
}
