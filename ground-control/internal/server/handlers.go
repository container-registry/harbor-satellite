package server

import (
	"crypto/rand"
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
	"container-registry.com/harbor-satellite/ground-control/internal/utils"
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
type RegisterSatelliteParams struct {
	Name   string    `json:"satellite_name"`
	Groups *[]string `json:"groups,omitempty"`
	Labels *[]string `json:"labels,omitempty"`
}
type AddSatelliteToGroupParams struct {
	SatelliteID int `json:"satellite_ID"`
	GroupID     int `json:"group_ID"`
}
type AddLabelToSatelliteParams struct {
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
	Image string `json:"image"`
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
		log.Println(err)
		HandleAppError(w, err)
		return
	}

	image, err := utils.ParseArtifactURL(req.Image)
	if err != nil {
		log.Println(err)
		err = &AppError{
			Message: "Error: Invalid Artifact URL",
			Code:    http.StatusBadRequest,
		}
		HandleAppError(w, err)
		return
	}

	params := database.AddImageParams{
		Registry:   image.Registry,
		Repository: image.Repository,
		Tag:        image.Tag,
		Digest:     image.Digest,
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

func (s *Server) listImageHandler(w http.ResponseWriter, r *http.Request) {
	result, err := s.dbQueries.ListImages(r.Context())
	if err != nil {
		err = fmt.Errorf("error: list images failed: %v", err)
		log.Println(err)
		HandleAppError(w, err)
		return
	}

	WriteJSONResponse(w, http.StatusOK, result)
}

func (s *Server) removeImageHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	imageID := vars["id"]

	id, err := strconv.ParseInt(imageID, 10, 32)
	if err != nil {
		err = fmt.Errorf("error: invalid imageID: %v", err)
		log.Println(err)
		HandleAppError(w, err)
		return
	}

	err = s.dbQueries.DeleteImage(r.Context(), int32(id))
	if err != nil {
		err = fmt.Errorf("error: delete image failed: %v", err)
		log.Println(err)
		HandleAppError(w, err)
		return
	}
	WriteJSONResponse(w, http.StatusNoContent, map[string]string{})
}

func (s *Server) registerSatelliteHandler(w http.ResponseWriter, r *http.Request) {
	var req RegisterSatelliteParams

	if err := DecodeRequestBody(r, &req); err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}

	token, err := GenerateRandomToken(32)
	if err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}

	// Start a new transaction
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}

	// Create a new Queries object bound to the transaction
	q := s.dbQueries.WithTx(tx)

	// Ensure proper transaction handling with defer
	defer func() {
		if p := recover(); p != nil {
			// If there's a panic, rollback the transaction
			tx.Rollback()
			panic(p) // Re-throw the panic after rolling back
		} else if err != nil {
			tx.Rollback() // Rollback transaction on error
		}
	}()

	// Create satellite
	satellite, err := q.CreateSatellite(r.Context(), req.Name)
	if err != nil {
		log.Println(err)
		err := &AppError{
			Message: fmt.Sprintf("Error: %v", err.Error()),
			Code:    http.StatusBadRequest,
		}
		HandleAppError(w, err)
		tx.Rollback()
		return
	}

	// Check if Groups is nil before dereferencing
	if req.Groups != nil {
		// Add satellite to groups
		for _, groupName := range *req.Groups {
			group, err := q.GetGroupByName(r.Context(), groupName)
			if err != nil {
				log.Println(err)
				err := &AppError{
					Message: fmt.Sprintf("Error: Invalid Group Name: %v", groupName),
					Code:    http.StatusBadRequest,
				}
				HandleAppError(w, err)
				tx.Rollback()
				return
			}
			err = q.AddSatelliteToGroup(r.Context(), database.AddSatelliteToGroupParams{
				SatelliteID: satellite.ID,
				GroupID:     group.ID,
			})
			if err != nil {
				log.Println(err)
				HandleAppError(w, err)
				tx.Rollback()
				return
			}
		}
	}

	// Check if Labels is nil before dereferencing
	if req.Labels != nil {
		// Add labels to satellite
		for _, labelName := range *req.Labels {
			label, err := q.GetLabelByName(r.Context(), labelName)
			if err != nil {
				log.Println(err)
				err := &AppError{
					Message: fmt.Sprintf("Error: Invalid Label Name: %v", labelName),
					Code:    http.StatusBadRequest,
				}
				HandleAppError(w, err)
				tx.Rollback()
				return
			}
			err = q.AddLabelToSatellite(r.Context(), database.AddLabelToSatelliteParams{
				SatelliteID: satellite.ID,
				LabelID:     label.ID,
			})
			if err != nil {
				log.Println(err)
				HandleAppError(w, err)
				tx.Rollback()
				return
			}
		}
	}

	// Add token to DB
	tk, err := q.AddToken(r.Context(), database.AddTokenParams{
		SatelliteID: satellite.ID,
		Token:       token,
	})
	if err != nil {
		log.Println("error in token")
		log.Println(err)
		HandleAppError(w, err)
		tx.Rollback()
		return
	}

	// get projects from images.
	repos, err := q.GetReposOfSatellite(r.Context(), satellite.ID)
	if err != nil {
		log.Println("error in repos")
		log.Println(err)
		HandleAppError(w, err)
		tx.Rollback()
		return
	}

	robot, err := utils.CreateRobotAccForSatellite(r.Context(), repos, satellite.Name)
	if err != nil {
		log.Println("error in creating robot account")
		log.Println(err)
		HandleAppError(w, err)
		tx.Rollback()
		return
	}

	_, err = q.AddRobotAccount(r.Context(), database.AddRobotAccountParams{
		RobotName:   robot.Name,
		RobotSecret: robot.Secret,
		SatelliteID: satellite.ID,
	})
	if err != nil {
		log.Println("error in adding robot account to DB")
		log.Println(err)
		HandleAppError(w, err)
		tx.Rollback()
		return
	}

	tx.Commit()
	WriteJSONResponse(w, http.StatusOK, tk)
}

func (s *Server) ztrHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	token := vars["token"]

	// Start a new transaction
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}

	q := s.dbQueries.WithTx(tx)

	defer func() {
		if p := recover(); p != nil {
			// If there's a panic, rollback the transaction
			tx.Rollback()
			panic(p) // Re-throw the panic after rolling back
		} else if err != nil {
			tx.Rollback() // Rollback transaction on error
		}
	}()

	satelliteID, err := q.GetSatelliteIDByToken(r.Context(), token)
	if err != nil {
		log.Println("Invalid Satellite Token")
		log.Println(err)
		err := &AppError{
			Message: "Error: Invalid Token",
			Code:    http.StatusBadRequest,
		}
		HandleAppError(w, err)
		tx.Rollback()
		return
	}

	err = q.DeleteToken(r.Context(), token)
	if err != nil {
		log.Println("error deleting token")
		log.Println(err)
		HandleAppError(w, err)
		tx.Rollback()
		return
	}

	robot, err := q.GetRobotAccBySatelliteID(r.Context(), satelliteID)
	if err != nil {
		log.Println("Robot Account Not Found")
		log.Println(err)
		err := &AppError{
			Message: "Error: Robot Account Not Found",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		tx.Rollback()
		return
	}

	tx.Commit()
	WriteJSONResponse(w, http.StatusOK, robot)
}

func (s *Server) listSatelliteHandler(w http.ResponseWriter, r *http.Request) {
	result, err := s.dbQueries.ListSatellites(r.Context())
	if err != nil {
		HandleAppError(w, err)
		return
	}

	WriteJSONResponse(w, http.StatusOK, result)
}

func (s *Server) getSatelliteByID(w http.ResponseWriter, r *http.Request) {
	result, err := s.dbQueries.ListSatellites(r.Context())
	if err != nil {
		HandleAppError(w, err)
		return
	}

	WriteJSONResponse(w, http.StatusOK, result)
}

func (s *Server) deleteSatelliteByID(w http.ResponseWriter, r *http.Request) {
	result, err := s.dbQueries.ListSatellites(r.Context())
	if err != nil {
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

func (s *Server) AddLabelToSatellite(w http.ResponseWriter, r *http.Request) {
	var req AddLabelToSatelliteParams
	if err := DecodeRequestBody(r, &req); err != nil {
		HandleAppError(w, err)
		return
	}

	params := database.AddLabelToSatelliteParams{
		SatelliteID: int32(req.SatelliteID),
		LabelID:     int32(req.LabelID),
	}

	err := s.dbQueries.AddLabelToSatellite(r.Context(), params)
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

	WriteJSONResponse(w, http.StatusOK, map[string]string{})
}

func (s *Server) listGroupImages(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	groupID := vars["groupID"]

	id, err := strconv.ParseInt(groupID, 10, 32)
	if err != nil {
		log.Printf("Error: Invalid groupID: %v", err)
		err := &AppError{
			Message: "Error: Invalid GroupID",
			Code:    http.StatusBadRequest,
		}
		HandleAppError(w, err)
		return
	}

	result, err := s.dbQueries.GetImagesForGroup(r.Context(), int32(id))
	if err != nil {
		log.Printf("Error: Failed to get image for group: %v", err)
		HandleAppError(w, err)
		return
	}

	WriteJSONResponse(w, http.StatusOK, result)
}

// func (s *Server) GetImagesForSatellite(w http.ResponseWriter, r *http.Request) {
// 	token, err := GetAuthToken(r)
// 	if err != nil {
// 		HandleAppError(w, err)
// 		return
// 	}
// 	result, err := s.dbQueries.GetImagesForSatellite(r.Context(), token)
// 	if err != nil {
// 		log.Printf("Error: Failed to get image for satellite: %v", err)
// 		HandleAppError(w, err)
// 		return
// 	}
//
// 	WriteJSONResponse(w, http.StatusOK, result)
// }

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
	group := vars["group"]

	groupID, err := strconv.ParseInt(group, 10, 32)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	result, err := s.dbQueries.GetGroupByID(r.Context(), int32(groupID))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	WriteJSONResponse(w, http.StatusOK, result)
}

// creates a unique random API token of the specified length in bytes.
func GenerateRandomToken(charLength int) (string, error) {
	// The number of bytes needed to generate a token with the required number of hex characters
	byteLength := charLength / 2

	// Create a byte slice of the required length
	token := make([]byte, byteLength)
	_, err := rand.Read(token)
	if err != nil {
		return "", err
	}

	// Return the token as a hex-encoded string
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
