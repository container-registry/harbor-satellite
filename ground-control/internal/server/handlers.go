package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"

	"container-registry.com/harbor-satellite/ground-control/internal/database"
)

type GroupRequestParams struct {
	GroupName string `json:"group_name"`
	Username  string `json:"username"`
	Password  string `json:"password"`
}

type GetGroupRequest struct {
	GroupName string `json:"group_name"`
}

type ImageListReqParams struct {
	GroupName string          `json:"group_name"`
	ImageList json.RawMessage `json:"image_list"`
}

func (s *Server) Ping(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte("pong"))
}

func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	err := s.db.Ping()
	if err != nil {
		log.Printf("error pinging db: %v", err)
		msg, _ := json.Marshal(map[string]string{"status": "unhealthy"})
		http.Error(w, string(msg), http.StatusBadRequest)
		return
	}

	jsonResp, err := json.Marshal(map[string]string{"status": "healthy"})
	if err != nil {
		log.Fatalf("error handling JSON marshal. Err: %v", err)
	}
	_, _ = w.Write(jsonResp)
}

func (s *Server) createGroupHandler(w http.ResponseWriter, r *http.Request) {
	// Decode request body
	var req GroupRequestParams
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	params := database.CreateGroupParams{
		GroupName: req.GroupName,
		Username:  req.Username,
		Password:  req.Password,
	}

	// Call the database query to create Group
	result, err := s.dbQueries.CreateGroup(r.Context(), params)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(result)
	if err != nil {
		log.Fatalf("error handling JSON marshal. Err: %v", err)
	}
}

func (s *Server) listGroupHandler(w http.ResponseWriter, r *http.Request) {
	result, err := s.dbQueries.ListGroups(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(result)
	if err != nil {
		log.Fatalf("error handling JSON marshal. Err: %v", err)
	}
}

func (s *Server) getGroupHandler(w http.ResponseWriter, r *http.Request) {
	var req GetGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	result, err := s.dbQueries.GetGroup(r.Context(), req.GroupName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(result)
	if err != nil {
		log.Fatalf("error handling JSON marshal. Err: %v", err)
	}
}

func (s *Server) getImageListHandler(w http.ResponseWriter, r *http.Request) {
	groupName := r.Header.Get("GroupName")
	username := r.Header.Get("Username")
	password := r.Header.Get("Password")
	if groupName == "" || username == "" || password == "" {
		http.Error(w, "Missing Authentication header params", http.StatusBadRequest)
		return
	}

	params := database.AuthenticateParams{
		GroupName: groupName,
		Username:  username,
		Password:  password,
	}

	group_id, err := s.dbQueries.Authenticate(r.Context(), params)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	result, err := s.dbQueries.GetImageList(r.Context(), group_id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(result)
	if err != nil {
		log.Fatalf("error handling JSON marshal. Err: %v", err)
	}
}

func (s *Server) addImageListHandler(w http.ResponseWriter, r *http.Request) {
	// Read the body of the request
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Unable to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Unmarshal the JSON into the struct
	var params ImageListReqParams
	err = json.Unmarshal(body, &params)
	if err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	group_id, err := s.dbQueries.GetGroupID(r.Context(), params.GroupName)
	if err != nil {
		log.Printf("error in getting group_id for %v, Err: %v", params.GroupName, err)
		http.Error(
			w,
			fmt.Sprintf("Invalid group: %v, Err: %v", params.GroupName, err),
			http.StatusNotFound,
		)
		return
	}

	reqParams := database.AddImageListParams{
		GroupID:   group_id,
		ImageList: params.ImageList,
	}

	err = s.dbQueries.AddImageList(r.Context(), reqParams)
	if err != nil {
		http.Error(
			w,
			fmt.Sprintf("Error adding Image List. Err: %v", err),
			http.StatusInternalServerError,
		)
		return
	}

	w.WriteHeader(http.StatusCreated)
}
