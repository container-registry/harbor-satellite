package server

import (
	"net/http"

	"github.com/gorilla/mux"
)

func (s *Server) RegisterRoutes() http.Handler {
	r := mux.NewRouter()

	r.HandleFunc("/ping", s.Ping).Methods("GET")
	r.HandleFunc("/health", s.healthHandler).Methods("GET")

	r.HandleFunc("/repos/list", s.regListHandler).Methods("GET")

	// Ground Control interface
	r.HandleFunc("/groups/list", s.listGroupHandler).Methods("GET")
	r.HandleFunc("/groups/{group}", s.getGroupHandler).Methods("GET")
	r.HandleFunc("/groups", s.createGroupHandler).Methods("POST")
	r.HandleFunc("/groups/satellite", s.addSatelliteToGroup).Methods("POST")
	r.HandleFunc("/groups/images", s.assignImageToGroup).Methods("POST")

	r.HandleFunc("/image", s.addImageHandler).Methods("POST")
	// r.HandleFunc("/satellites", s.addSatelliteHandler).Methods("POST")

	r.HandleFunc("/labels", s.createLabelHandler).Methods("POST")
	r.HandleFunc("/label/satellite", s.addSatelliteToLabel).Methods("POST")
	r.HandleFunc("/label/images", s.assignImageToLabel).Methods("POST")

	r.HandleFunc("/satellites/register", s.registerSatelliteHandler).Methods("POST")
	r.HandleFunc("/satellites/ztr", s.ztrHandler).Methods("POST")
	r.HandleFunc("/satellites/list", s.listSatelliteHandler).Methods("GET")
	r.HandleFunc("/satellites/{satellite}", s.getSatelliteByID).Methods("GET")
	r.HandleFunc("/satellites/{satellite}", s.deleteSatelliteByID).Methods("DELETE")
	// r.HandleFunc("/satellites/{satellite}/images", s.GetImagesForSatellite).Methods("GET")

	return r
}
