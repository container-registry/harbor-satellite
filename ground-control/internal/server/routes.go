package server

import (
	"net/http"

	"github.com/gorilla/mux"
)

func (s *Server) RegisterRoutes() http.Handler {
	r := mux.NewRouter()

	r.HandleFunc("/ping", s.Ping).Methods("GET")
	r.HandleFunc("/health", s.healthHandler).Methods("GET")

	// Ground Control interface
	r.HandleFunc("/groups/sync", s.groupsSyncHandler).Methods("POST")
	r.HandleFunc("/groups/list", s.listGroupHandler).Methods("GET")
	r.HandleFunc("/groups/{group}", s.getGroupHandler).Methods("GET")
	r.HandleFunc("/groups/satellite", s.addSatelliteToGroup).Methods("POST")
	r.HandleFunc("/groups/satellite", s.removeSatelliteFromGroup).Methods("DELETE")

	// to-do: listing functionality to list satellites attached to group
	// for ground control admins
	// r.HandleFunc("/groups/{group}/list", s.groupSatelliteHandler).Methods("GET")

	// Ground Control interface
	r.HandleFunc("/satellites/register", s.registerSatelliteHandler).Methods("POST")
	r.HandleFunc("/satellites/ztr/{token}", s.ztrHandler).Methods("GET")
	r.HandleFunc("/satellites/list", s.listSatelliteHandler).Methods("GET")
	r.HandleFunc("/satellites/{satellite}", s.GetSatelliteByName).Methods("GET")
	r.HandleFunc("/satellites/{satellite}", s.DeleteSatelliteByName).Methods("DELETE")
	r.HandleFunc("/satellites/{satellite}/sync", s.satelliteSyncHandler).Methods("GET")
	// r.HandleFunc("/satellites/{satellite}/images", s.GetImagesForSatellite).Methods("GET")

	return r
}
