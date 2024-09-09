package server

import (
	"net/http"

	"github.com/gorilla/mux"
)

func (s *Server) RegisterRoutes() http.Handler {
	r := mux.NewRouter()

	r.HandleFunc("/ping", s.Ping).Methods("GET")
	r.HandleFunc("/health", s.healthHandler).Methods("GET")

	r.HandleFunc("/registry/list", s.regListHandler).Methods("GET")

	r.HandleFunc("/image", s.addImageHandler).Methods("POST")

	// Ground Control interface
	r.HandleFunc("/groups/create", s.createGroupHandler).Methods("POST")
	r.HandleFunc("/groups/list", s.listGroupHandler).Methods("GET")
	r.HandleFunc("/groups/{group}", s.getGroupHandler).Methods("GET")
	r.HandleFunc("/groups/images", s.assignImageToGroup).Methods("POST")
	r.HandleFunc("/groups/images", s.deleteImageFromGroup).Methods("DELETE")
	r.HandleFunc("/groups/satellite", s.addSatelliteToGroup).Methods("POST")
	r.HandleFunc("/groups/satellite", s.removeSatelliteFromGroup).Methods("DELETE")

	r.HandleFunc("/label", s.createLabelHandler).Methods("POST")
	r.HandleFunc("/label/images", s.assignImageToLabel).Methods("POST")
	r.HandleFunc("/label/images", s.deleteImageFromLabel).Methods("DELETE")
	r.HandleFunc("/label/satellite", s.addSatelliteToLabel).Methods("POST")

	r.HandleFunc("/satellites/create", s.addSatelliteHandler).Methods("POST")
	r.HandleFunc("/satellites/list", s.listSatelliteHandler).Methods("GET")
	r.HandleFunc("/satellites/{satellite}", s.getSatelliteByID).Methods("GET")
	r.HandleFunc("/satellites/{satellite}", s.deleteSatelliteByID).Methods("DELETE")
	// r.HandleFunc("/satellites/images", s.GetImagesForSatellite).Methods("GET")
	r.HandleFunc("/satellites/images", s.assignImageToSatellite).Methods("POST")
	r.HandleFunc("/satellites/images", s.removeImageFromSatellite).Methods("DELETE")

	// Satellite based routes
	// r.HandleFunc("/images", s.getImageListHandler).Methods("GET")
	// r.HandleFunc("/images", s.addImageListHandler).Methods("POST")
	// r.HandleFunc("/group", s.deleteGroupHandler).Methods("DELETE")

	return r
}
