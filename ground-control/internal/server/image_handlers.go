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
	Page     int `json:"page" query:"page"`
	PageSize int `json:"page_size" query:"page_size"`
}

type ImageDistributionResponse struct {
	ImageCount          int `json:"image_count"`
	ReportingSatellites int `json:"reporting_satellites"`
	ReportingGroups     int `json:"reporting_groups"`
	// TODO: Digest, last_seen,
	Images []database.GetImageDistributionRow `json:"images"`
}

func (s *Server) getImageDistribution(w http.ResponseWriter, r *http.Request) {
	req := ImageDistributionParams{
		Page:     1,
		PageSize: 50,
	}
	if err := DecodeRequestParams(r, &req, r.PathValue); err != nil {
		log.Println("Error decoding request params:", err)
		HandleAppError(w, err)
		return
	}

	result, err := s.dbQueries.GetImageDistribution(r.Context(), database.GetImageDistributionParams{
		Limit:  int32(req.PageSize),
		Offset: int32((req.Page - 1) * req.PageSize),
	})
	if err != nil {
		log.Printf("Could not get image distribution: %v", err)
		WriteJSONError(w, "error providing image distribution", http.StatusInternalServerError)
		return
	}

	var (
		reportingSatellites = map[string]bool{}
		reportingGroups     = map[string]bool{}
	)
	for _, v := range result {
		for _, sat := range v.Satellites {
			if _, ok := reportingSatellites[sat]; !ok {
				reportingSatellites[sat] = true
			}
		}

		for _, grp := range v.Groups {
			if _, ok := reportingGroups[grp]; !ok {
				reportingGroups[grp] = true
			}
		}
	}

	WriteJSONResponse(w, http.StatusOK, ImageDistributionResponse{
		ImageCount:          len(result),
		ReportingSatellites: len(reportingSatellites),
		ReportingGroups:     len(reportingGroups),
		Images:              result,
	})
}
