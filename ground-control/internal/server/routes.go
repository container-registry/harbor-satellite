package server

import (
	"net/http"

	"github.com/gorilla/mux"
)

func (s *Server) RegisterRoutes() http.Handler {
	r := mux.NewRouter()

	r.HandleFunc("/ping", s.Ping).Methods("GET")
	r.HandleFunc("/health", s.healthHandler).Methods("GET")

	// Groups
	r.HandleFunc("/groups", s.listGroupHandler).Methods("GET")        // List all groups
	r.HandleFunc("/groups/sync", s.groupsSyncHandler).Methods("POST") // Sync groups
	r.HandleFunc("/groups/{group}", s.getGroupHandler).Methods("GET") // Get specific group

	// Satellites in groups
	r.HandleFunc("/groups/{group}/satellites", s.groupSatelliteHandler).Methods("GET") // List satellites in group
	r.HandleFunc("/groups/satellite", s.addSatelliteToGroup).Methods("POST")           // Add satellite to group
	r.HandleFunc("/groups/satellite", s.removeSatelliteFromGroup).Methods("DELETE")    // Remove satellite from group

	// Configs
	r.HandleFunc("/configs", s.listConfigsHandler).Methods("GET")
	r.HandleFunc("/configs", s.createConfigHandler).Methods("POST")
	r.HandleFunc("/configs/{config}", s.updateConfigHandler).Methods("PATCH")
	r.HandleFunc("/configs/{config}", s.getConfigHandler).Methods("GET")
	r.HandleFunc("/configs/{config}", s.deleteConfigHandler).Methods("DELETE")
	r.HandleFunc("/configs/satellite", s.setSatelliteConfig).Methods("POST")

	// Satellites
	r.HandleFunc("/satellites", s.listSatelliteHandler).Methods("GET")      // List all satellites
	r.HandleFunc("/satellites", s.registerSatelliteHandler).Methods("POST") // Register new satellite
	r.HandleFunc("/satellites/ztr/{token}", s.ztrHandler).Methods("GET")
	r.HandleFunc("/satellites/{satellite}", s.GetSatelliteByName).Methods("GET")       // Get specific satellite
	r.HandleFunc("/satellites/{satellite}", s.DeleteSatelliteByName).Methods("DELETE") // Delete specific satellite
	// r.HandleFunc("/satellites/{satellite}/images", s.GetImagesForSatellite).Methods("GET") // Get satellite images
	r.HandleFunc("/satellites/status",s.statusReportHandler).Methods("POST")
	return r
}
