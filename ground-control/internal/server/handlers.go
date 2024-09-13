package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"container-registry.com/harbor-satellite/ground-control/internal/database"
	"container-registry.com/harbor-satellite/ground-control/reg"
	"github.com/gorilla/mux"
)

type RegListParams struct {
	Url      string `json:"registry_url"`
	UserName string `json:"username"`
	Password string `json:"password"`
}
type GroupRequestParams struct {
	GroupName string `json:"group_name"`
}
type LabelRequestParams struct {
	LabelName string `json:"label_name"`
}
type AddSatelliteParams struct {
	Name string `json:"name"`
}
type AddSatelliteToGroupParams struct {
	SatelliteID int `json:"satellite_ID"`
	GroupID     int `json:"group_ID"`
}
type AddSatelliteToLabelParams struct {
	SatelliteID int `json:"satellite_ID"`
	LabelID     int `json:"label_ID"`
}
type AssignImageToLabelParams struct {
	LabelID int `json:"label_ID"`
	ImageID int `json:"image_ID"`
}
type AssignImageToGroupParams struct {
	GroupID int32 `json:"group_ID"`
	ImageID int32 `json:"image_ID"`
}
type ImageAddParams struct {
	Registry   string `json:"registry"`
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
	Digest     string `json:"digest"`
}
type GetGroupRequest struct {
	GroupName string `json:"group_name"`
}
type ImageListReqParams struct {
	GroupName string          `json:"group_name"`
	ImageList json.RawMessage `json:"image_list"`
}

func DecodeRequestBody(r *http.Request, v interface{}) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return &AppError{
			Message: "Invalid request body",
			Code:    http.StatusBadRequest,
		}
	}
	return nil
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

	WriteJSONResponse(w, http.StatusOK, map[string]string{"status": "healthy"})
}

func (s *Server) createGroupHandler(w http.ResponseWriter, r *http.Request) {
	// Decode request body
	var req GroupRequestParams
	if err := DecodeRequestBody(r, &req); err != nil {
		HandleAppError(w, err)
		return
	}

	params := database.CreateGroupParams{
		GroupName: req.GroupName,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// Call the database query to create Group
	result, err := s.dbQueries.CreateGroup(r.Context(), params)
	if err != nil {
		err = &AppError{
			Message: err.Error(),
			Code:    http.StatusBadRequest,
		}
		HandleAppError(w, err)
		return
	}

	WriteJSONResponse(w, http.StatusCreated, result)
}

func (s *Server) createLabelHandler(w http.ResponseWriter, r *http.Request) {
	var req LabelRequestParams
	if err := DecodeRequestBody(r, &req); err != nil {
		HandleAppError(w, err)
		return
	}
	params := database.CreateLabelParams{
		LabelName: req.LabelName,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	result, err := s.dbQueries.CreateLabel(r.Context(), params)
	if err != nil {
		HandleAppError(w, err)
		return
	}

	WriteJSONResponse(w, http.StatusCreated, result)
}

func (s *Server) addImageHandler(w http.ResponseWriter, r *http.Request) {
	var req ImageAddParams
	if err := DecodeRequestBody(r, &req); err != nil {
		HandleAppError(w, err)
		return
	}

	params := database.AddImageParams{
		Registry:   req.Registry,
		Repository: req.Repository,
		Tag:        req.Tag,
		Digest:     req.Digest,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	// Call the database query to create Group
	result, err := s.dbQueries.AddImage(r.Context(), params)
	if err != nil {
		HandleAppError(w, err)
		return
	}

	WriteJSONResponse(w, http.StatusCreated, result)
}

func (s *Server) addSatelliteHandler(w http.ResponseWriter, r *http.Request) {
	var req AddSatelliteParams
	if err := DecodeRequestBody(r, &req); err != nil {
		HandleAppError(w, err)
		return
	}

	token, err := GenerateRandomToken(32)
	if err != nil {
		HandleAppError(w, err)
	}

	params := database.CreateSatelliteParams{
		Name:  req.Name,
		Token: token,
	}

	result, err := s.dbQueries.CreateSatellite(r.Context(), params)
	if err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}

	WriteJSONResponse(w, http.StatusOK, result)
}

func (s *Server) addSatelliteToGroup(w http.ResponseWriter, r *http.Request) {
	var req AddSatelliteToGroupParams
	if err := DecodeRequestBody(r, &req); err != nil {
		HandleAppError(w, err)
		return
	}

	params := database.AddSatelliteToGroupParams{
		SatelliteID: int32(req.SatelliteID),
		GroupID:     int32(req.GroupID),
	}

	err := s.dbQueries.AddSatelliteToGroup(r.Context(), params)
	if err != nil {
		log.Printf("Error: Failed to Add Satellite to Group: %v", err)
		HandleAppError(w, err)
		return
	}

	WriteJSONResponse(w, http.StatusOK, map[string]string{})
}

func (s *Server) addSatelliteToLabel(w http.ResponseWriter, r *http.Request) {
	var req AddSatelliteToLabelParams
	if err := DecodeRequestBody(r, &req); err != nil {
		HandleAppError(w, err)
		return
	}

	params := database.AddSatelliteToLabelParams{
		SatelliteID: int32(req.SatelliteID),
		LabelID:     int32(req.LabelID),
	}

	err := s.dbQueries.AddSatelliteToLabel(r.Context(), params)
	if err != nil {
		log.Printf("Error: Failed to add satellite to label: %v", err)
		HandleAppError(w, err)
		return
	}

	WriteJSONResponse(w, http.StatusOK, map[string]string{})
}

func (s *Server) assignImageToLabel(w http.ResponseWriter, r *http.Request) {
	var req AssignImageToLabelParams
	if err := DecodeRequestBody(r, &req); err != nil {
		HandleAppError(w, err)
		return
	}

	params := database.AssignImageToLabelParams{
		LabelID: int32(req.LabelID),
		ImageID: int32(req.ImageID),
	}

	err := s.dbQueries.AssignImageToLabel(r.Context(), params)
	if err != nil {
		log.Printf("Error: Failed to assign image to label: %v", err)
		HandleAppError(w, err)
		return
	}

	WriteJSONResponse(w, http.StatusOK, map[string]string{})
}

func (s *Server) assignImageToGroup(w http.ResponseWriter, r *http.Request) {
	var req AssignImageToGroupParams
	if err := DecodeRequestBody(r, &req); err != nil {
		HandleAppError(w, err)
		return
	}

	params := database.AssignImageToGroupParams{
		GroupID: int32(req.GroupID),
		ImageID: int32(req.ImageID),
	}

	err := s.dbQueries.AssignImageToGroup(r.Context(), params)
	if err != nil {
		log.Printf("Error: Failed to assign image to group: %v", err)
		HandleAppError(w, err)
		return
	}

	WriteJSONResponse(w, http.StatusOK, map[string]string{})
}

func (s *Server) GetImagesForSatellite(w http.ResponseWriter, r *http.Request) {
	token, err := GetAuthToken(r)
	if err != nil {
		HandleAppError(w, err)
		return
	}
	result, err := s.dbQueries.GetImagesForSatellite(r.Context(), token)
	if err != nil {
		log.Printf("Error: Failed to get image for satellite: %v", err)
		HandleAppError(w, err)
		return
	}

	WriteJSONResponse(w, http.StatusOK, result)
}

func (s *Server) listGroupHandler(w http.ResponseWriter, r *http.Request) {
	result, err := s.dbQueries.ListGroups(r.Context())
	if err != nil {
		HandleAppError(w, err)
		return
	}

	WriteJSONResponse(w, http.StatusOK, result)
}

func (s *Server) regListHandler(w http.ResponseWriter, r *http.Request) {
	username := r.URL.Query().Get("username")
	password := r.URL.Query().Get("password")
	url := r.URL.Query().Get("url")

	if url == "" {
		err := &AppError{
			Message: "Missing URL in Request",
			Code:    http.StatusBadRequest,
		}
		HandleAppError(w, err)
		return
	}

	result, err := reg.FetchRepos(username, password, url)
	if err != nil {
		HandleAppError(w, err)
		return
	}

	WriteJSONResponse(w, http.StatusOK, result)
}

func (s *Server) getGroupHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	groupName := vars["group"]

	result, err := s.dbQueries.GetGroupByName(r.Context(), groupName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	WriteJSONResponse(w, http.StatusOK, result)
}

// creates a unique random API token of the specified length in bytes.
func GenerateRandomToken(length int) (string, error) {
	token := make([]byte, length)
	_, err := rand.Read(token)
	if err != nil {
		return "", err
	}

	return hex.EncodeToString(token), nil
}

func GetAuthToken(r *http.Request) (string, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		err := &AppError{
			Message: "Authorization header missing",
			Code:    http.StatusUnauthorized,
		}
		return "", err
	}

	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		err := &AppError{
			Message: "Invalid Authorization header format",
			Code:    http.StatusUnauthorized,
		}
		return "", err
	}
	token := parts[1]

	return token, nil
}
