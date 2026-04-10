package server

import (
	"log"
	"net/http"

	"github.com/container-registry/harbor-satellite/ground-control/internal/database"
)

// TODO: Add:-
//
//  1. Pagination
//  2. Group/Satellite/Pattern based filtering
//  3. Output Format
type ImageDistributionParams struct {
}

type ImageDistributionResponse struct {
	ImageCount int `json:"image_count"`
	// TODO: Digest, last_seen,
	Images []database.GetImageDistributionRow `json:"images"`
}

func (s *Server) getImageDistribution(w http.ResponseWriter, r *http.Request) {
	var req ImageDistributionParams

	if err := DecodeRequestBody(r, &req); err != nil {
		log.Println("Error decoding request body:", err)
		HandleAppError(w, err)
		return
	}

	result, err := s.dbQueries.GetImageDistribution(r.Context())
	if err != nil {
		log.Printf("Could not get group: %v", err)
		WriteJSONError(w, "Group not found", http.StatusNotFound)
		return
	}

	WriteJSONResponse(w, http.StatusOK, ImageDistributionResponse{
		ImageCount: len(result),
		Images:     result,
	})
}
