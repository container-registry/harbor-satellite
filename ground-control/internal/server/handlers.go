package server

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
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
	GroupName     string `json:"group_name"`
	ParentGroupID *int   `json:"parent_group_id,omitempty"`
}
type LabelRequestParams struct {
	LabelName string `json:"label_name"`
}
type AddSatelliteParams struct {
	Name string `json:"name"`
}
type AddSatelliteToGroupParams struct {
	SatelliteID int `json:"satellite_id"`
	GroupID     int `json:"group_id"`
}
type AddSatelliteToLabelParams struct {
	SatelliteID int `json:"satellite_id"`
	LabelID     int `json:"label_id"`
}
type AssignImageToLabelParams struct {
	LabelID int `json:"label_id"`
	ImageID int `json:"image_id"`
}
type AssignImageToGroupParams struct {
	GroupID int32 `json:"group_id"`
	ImageID int32 `json:"image_id"`
}
type AssignImageToSatelliteParams struct {
	SatelliteID int32 `json:"group_id"`
	ImageID     int32 `json:"image_id"`
}
type ImageAddParams struct {
	Registry   string `json:"registry"`
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
	Digest     string `json:"digest"`
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
	// Convert ParentGroupID to sql.NullInt32
	var parentGroupID sql.NullInt32
	if req.ParentGroupID != nil {
		parentGroupID = sql.NullInt32{
			Int32: int32(*req.ParentGroupID),
			Valid: true,
		}
	} else {
		parentGroupID = sql.NullInt32{Valid: false} // Mark as invalid if null
	}

	params := database.CreateGroupParams{
		ParentGroupID: parentGroupID,
		GroupName:     req.GroupName,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
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

func (s *Server) listSatelliteHandler(w http.ResponseWriter, r *http.Request) {
	result, err := s.dbQueries.ListSatellites(r.Context())
	if err != nil {
		log.Printf("error: error listing satellites: %v", err)
		HandleAppError(w, err)
		return
	}

	WriteJSONResponse(w, http.StatusOK, result)
}

func (s *Server) getSatelliteByID(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	satelliteID := vars["satellite"]
	id, err := strconv.ParseInt(satelliteID, 10, 32)
	if err != nil {
		log.Printf("error: invalid satellite ID: %v", err)
		HandleAppError(w, err)
		return
	}

	result, err := s.dbQueries.GetSatelliteByID(r.Context(), int32(id))
	if err != nil {
		log.Printf("error: error getting satellites: %v", err)
		HandleAppError(w, err)
		return
	}

	WriteJSONResponse(w, http.StatusOK, result)
}

func (s *Server) deleteSatelliteByID(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	satelliteID := vars["satellite"]

	id, err := strconv.ParseInt(satelliteID, 10, 32)
	if err != nil {
		log.Printf("error: id is invalid: %v", err)
		HandleAppError(w, err)
		return
	}

	result := s.dbQueries.DeleteSatellite(r.Context(), int32(id))

	WriteJSONResponse(w, http.StatusOK, result)
}

func (s *Server) assignImageToSatellite(w http.ResponseWriter, r *http.Request) {
	var req AssignImageToSatelliteParams
	if err := DecodeRequestBody(r, &req); err != nil {
		HandleAppError(w, err)
		return
	}

	params := database.AssignImageToSatelliteParams{
		SatelliteID: int32(req.SatelliteID),
		ImageID:     int32(req.ImageID),
	}

	err := s.dbQueries.AssignImageToSatellite(r.Context(), params)
	if err != nil {
		log.Printf("error: failed to assign image to satellite: %v", err)
		HandleAppError(w, err)
		return
	}

	err = s.createSatelliteArtifact(r.Context(), req.SatelliteID)
	if err != nil {
		log.Printf("error: failed to create satellite state artifact: %v", err)
		err = &AppError{
			Message: "Error in State Artifact: Please create project named 'satellite' for storing Satellite State Artifact",
			Code:    http.StatusBadRequest,
		}
		HandleAppError(w, err)
		return
	}

	WriteJSONResponse(w, http.StatusOK, map[string]string{})
}

func (s *Server) removeImageFromSatellite(w http.ResponseWriter, r *http.Request) {
	var req AssignImageToSatelliteParams
	if err := DecodeRequestBody(r, &req); err != nil {
		HandleAppError(w, err)
		return
	}

	params := database.RemoveImageFromSatelliteParams{
		SatelliteID: int32(req.SatelliteID),
		ImageID:     int32(req.ImageID),
	}

	err := s.dbQueries.RemoveImageFromSatellite(r.Context(), params)
	if err != nil {
		log.Printf("Error: Failed to delete image from group: %v", err)
		HandleAppError(w, err)
		return
	}

	err = s.createSatelliteArtifact(r.Context(), req.SatelliteID)
	if err != nil {
		log.Printf("error: failed to create state artifact: %v", err)
		log.Printf("adding deleted image back to satellite: %v", err)
		err = s.dbQueries.AssignImageToSatellite(r.Context(), database.AssignImageToSatelliteParams{
			SatelliteID: int32(req.SatelliteID),
			ImageID:     int32(req.ImageID),
		})
		err = &AppError{
			Message: "Error in State Artifact: Please create project named 'satellite' in registry for storing Group State Artifact",
			Code:    http.StatusBadRequest,
		}
		HandleAppError(w, err)
		return
	}

	WriteJSONResponse(w, http.StatusOK, map[string]string{})
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

func (s *Server) removeSatelliteFromGroup(w http.ResponseWriter, r *http.Request) {
	var req AddSatelliteToGroupParams
	if err := DecodeRequestBody(r, &req); err != nil {
		HandleAppError(w, err)
		return
	}

	params := database.RemoveSatelliteFromGroupParams{
		SatelliteID: int32(req.SatelliteID),
		GroupID:     int32(req.GroupID),
	}

	err := s.dbQueries.RemoveSatelliteFromGroup(r.Context(), params)
	if err != nil {
		log.Printf("error: failed to remove satellite from group: %v", err)
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
		log.Printf("error: failed to assign image to group: %v", err)
		HandleAppError(w, err)
		return
	}

	err = s.createGroupArtifact(r.Context(), req.GroupID)
	if err != nil {
		log.Printf("error: failed to create state artifact: %v", err)
		err = &AppError{
			Message: "Error in State Artifact: Please create project named 'satellite' for storing Group State Artifact",
			Code:    http.StatusBadRequest,
		}
		HandleAppError(w, err)
		return
	}

	WriteJSONResponse(w, http.StatusOK, map[string]string{})
}

func (s *Server) deleteImageFromGroup(w http.ResponseWriter, r *http.Request) {
	var req AssignImageToGroupParams
	if err := DecodeRequestBody(r, &req); err != nil {
		HandleAppError(w, err)
		return
	}

	params := database.RemoveImageFromGroupParams{
		GroupID: int32(req.GroupID),
		ImageID: int32(req.ImageID),
	}

	err := s.dbQueries.RemoveImageFromGroup(r.Context(), params)
	if err != nil {
		log.Printf("Error: Failed to delete image from group: %v", err)
		HandleAppError(w, err)
		return
	}

	err = s.createGroupArtifact(r.Context(), req.GroupID)
	if err != nil {
		log.Printf("error: failed to create state artifact: %v", err)
		log.Printf("adding deleted image back to group: %v", err)
		err = s.dbQueries.AssignImageToGroup(r.Context(), database.AssignImageToGroupParams{
			GroupID: req.GroupID,
			ImageID: req.ImageID,
		})
		err = &AppError{
			Message: "Error in State Artifact: Please create project named 'satellite' in registry for storing Group State Artifact",
			Code:    http.StatusBadRequest,
		}
		HandleAppError(w, err)
		return
	}

	WriteJSONResponse(w, http.StatusOK, map[string]string{})
}

func (s *Server) deleteImageFromLabel(w http.ResponseWriter, r *http.Request) {
	var req AssignImageToLabelParams
	if err := DecodeRequestBody(r, &req); err != nil {
		HandleAppError(w, err)
		return
	}

	params := database.RemoveImageFromLabelParams{
		LabelID: int32(req.LabelID),
		ImageID: int32(req.ImageID),
	}

	err := s.dbQueries.RemoveImageFromLabel(r.Context(), params)
	if err != nil {
		log.Printf("error: failed to remove image from label: %v", err)
		HandleAppError(w, err)
		return
	}

	WriteJSONResponse(w, http.StatusOK, map[string]string{})
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
	username := os.Getenv("HARBOR_USERNAME")
	password := os.Getenv("HARBOR_PASSWORD")
	url := os.Getenv("HARBOR_URL")

	if url == "" {
		err := &AppError{
			Message: "Missing URL in ENV",
			Code:    http.StatusBadRequest,
		}
		HandleAppError(w, err)
		return
	}

	url = fmt.Sprintf("https://%s", url)

	result, err := reg.FetchRepos(username, password, url)
	if err != nil {
		HandleAppError(w, err)
		return
	}

	WriteJSONResponse(w, http.StatusOK, result)
}

func (s *Server) createSatelliteArtifact(ctx context.Context, id int32) error {
	url := os.Getenv("HARBOR_URL")

	satellite, err := s.dbQueries.GetSatelliteByID(ctx, id)
	if err != nil {
		return fmt.Errorf("Error in getting satellite: %v", err)
	}

	res, err := s.dbQueries.GetImagesForSatellite(ctx, id)
	if err != nil {
		return fmt.Errorf("Error in getting images for group: %v", err)
	}

	groups, err := s.dbQueries.GetGroupsBySatelliteID(ctx, id)
	if err != nil {
		return fmt.Errorf("Error in getting images for group: %v", err)
	}

	var images []reg.Images
	for _, img := range res {
		image := reg.Images{
			Registry:   img.Registry,
			Repository: img.Repository,
			Tag:        img.Tag,
			Digest:     img.Digest,
		}
		images = append(images, image)
	}

  var groupsState []string
  for _, group := range groups {
    group := fmt.Sprintf("%s/satellite/groups/%s", url, group)
    groupsState = append(groupsState, group)
  }

	State := &reg.SatelliteState{
		Name:     satellite.Name,
		Groups:   groups,
		Registry: url,
		Images:   images,
	}

	err = reg.PushStateArtifact(ctx, *State)
	if err != nil {
		return fmt.Errorf("error in state artifact: %v", err)
	}

	return nil
}

func (s *Server) createGroupArtifact(ctx context.Context, groupID int32) error {
	url := os.Getenv("HARBOR_URL")

	group, err := s.dbQueries.GetGroupByID(ctx, groupID)
	if err != nil {
		return fmt.Errorf("Error in getting group: %v", err)
	}
	if group.ParentGroupID.Valid {
		if err := s.createGroupArtifact(ctx, group.ParentGroupID.Int32); err != nil {
			return fmt.Errorf(
				"error creating artifact for parent group %d: %v",
				group.ParentGroupID.Int32,
				err,
			)
		}
	}

	res, err := s.dbQueries.GetImagesForGroupAndSubgroups(ctx, groupID)
	if err != nil {
		return fmt.Errorf("Error in getting images for group: %v", err)
	}

	var images []reg.Images

	for _, img := range res {
		image := reg.Images{
			Registry:   img.Registry,
			Repository: img.Repository,
			Tag:        img.Tag,
			Digest:     img.Digest,
		}
		images = append(images, image)
	}

	State := &reg.GroupState{
		Name:     group.GroupName,
		Registry: url,
		Images:   images,
	}

	err = reg.PushStateArtifact(ctx, *State)
	if err != nil {
		return fmt.Errorf("error in state artifact: %v", err)
	}

	return nil
}

func (s *Server) getGroupHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	groupID := vars["group"]

  id, err := strconv.ParseInt(groupID, 10, 32)
	if err != nil {
    log.Printf("error: invalid groupID: %v", err)
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	result, err := s.dbQueries.GetGroupByID(r.Context(), int32(id))
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
