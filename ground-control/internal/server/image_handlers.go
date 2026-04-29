package server

import (
	"log"
	"net/http"
	"regexp"

	"github.com/container-registry/harbor-satellite/ground-control/internal/database"
)

type ImageDistributionParams struct {
	SatelliteFilter string `json:"satellite"`
	GroupFilter     string `json:"group"`
	ImageFilter     string `json:"image"`
	Page            int    `json:"page"`
	PageSize        int    `json:"page_size"`
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
	if err := DecodeRequestBody(r, &req); err != nil {
		log.Println("Error decoding request params:", err)
		HandleAppError(w, err)
		return
	}

	params := database.GetImageDistributionParams{
		Limit:  int32(req.PageSize),
		Offset: int32((req.Page - 1) * req.PageSize),
	}
	result, err := s.dbQueries.GetImageDistribution(r.Context(), params)
	if err != nil {
		log.Printf("Could not get image distribution: %v", err)
		WriteJSONError(w, "error providing image distribution", http.StatusInternalServerError)
		return
	}

	// Compiling RegEx
	satReg, err := regexp.Compile(req.SatelliteFilter)
	if err != nil {
		log.Printf("regex compilation failed: %v", err)
		WriteJSONError(w, "error creating regex for satellite", http.StatusInternalServerError)
		return
	}
	grpReg, err := regexp.Compile(req.GroupFilter)
	if err != nil {
		log.Printf("regex compilation failed: %v", err)
		WriteJSONError(w, "error creating regex for group", http.StatusInternalServerError)
		return
	}

	// Filtering
	filtered := make([]database.GetImageDistributionRow, 0)
	for _, art := range result {
		if matchRegexFilter(art.Satellites, satReg) &&
			matchRegexFilter(art.Groups, grpReg) &&
			matchStringFilter(art.Reference, req.ImageFilter) {
			filtered = append(filtered, art)
		}
	}

	// Calculating Count
	var (
		reportingSatellites = map[string]bool{}
		reportingGroups     = map[string]bool{}
	)
	for _, v := range filtered {
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
		ImageCount:          len(filtered),
		ReportingSatellites: len(reportingSatellites),
		ReportingGroups:     len(reportingGroups),
		Images:              filtered,
	})
}
