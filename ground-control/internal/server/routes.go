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
	r.HandleFunc("/group", s.createGroupHandler).Methods("POST")
	r.HandleFunc("/group/list", s.listGroupHandler).Methods("GET")
	r.HandleFunc("/group/{group}", s.getGroupHandler).Methods("GET")
	r.HandleFunc("/group/images", s.assignImageToGroup).Methods("POST")
  r.HandleFunc("/group/images", s.deleteImageFromGroup).Methods("DELETE")
	r.HandleFunc("/group/satellite", s.addSatelliteToGroup).Methods("POST")

	r.HandleFunc("/label", s.createLabelHandler).Methods("POST")
	r.HandleFunc("/label/images", s.assignImageToLabel).Methods("POST")
	r.HandleFunc("/label/images", s.deleteImageFromLabel).Methods("DELETE")
	r.HandleFunc("/label/satellite", s.addSatelliteToLabel).Methods("POST")


	r.HandleFunc("/satellite", s.addSatelliteHandler).Methods("POST")
	r.HandleFunc("/satellite/images", s.GetImagesForSatellite).Methods("GET")

	// Satellite based routes
	// r.HandleFunc("/images", s.getImageListHandler).Methods("GET")
	// r.HandleFunc("/images", s.addImageListHandler).Methods("POST")
	// r.HandleFunc("/group", s.deleteGroupHandler).Methods("DELETE")

	return r
}
