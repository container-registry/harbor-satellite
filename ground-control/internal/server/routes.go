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
  r.HandleFunc("/groups/images", s.deleteImageFromGroup).Methods("DELETE")
	r.HandleFunc("/groups/{groupID}/images", s.listGroupImages).Methods("GET")


	r.HandleFunc("/images", s.addImageHandler).Methods("POST")
	r.HandleFunc("/images/list", s.listImageHandler).Methods("GET")
	r.HandleFunc("/images/{id}", s.removeImageHandler).Methods("DELETE")
	// r.HandleFunc("/satellites", s.addSatelliteHandler).Methods("POST")

	r.HandleFunc("/labels", s.createLabelHandler).Methods("POST")
	r.HandleFunc("/labels/images", s.assignImageToLabel).Methods("POST")

	r.HandleFunc("/satellites/register", s.registerSatelliteHandler).Methods("POST")
	r.HandleFunc("/satellites/ztr/{token}", s.ztrHandler).Methods("GET")
	r.HandleFunc("/satellites/list", s.listSatelliteHandler).Methods("GET")
	r.HandleFunc("/satellites/{satellite}", s.getSatelliteByID).Methods("GET")
	r.HandleFunc("/satellites/labels", s.AddLabelToSatellite).Methods("POST")
	r.HandleFunc("/satellites/{satellite}", s.deleteSatelliteByID).Methods("DELETE")
	// r.HandleFunc("/satellites/{satellite}/images", s.GetImagesForSatellite).Methods("GET")

	return r
}
