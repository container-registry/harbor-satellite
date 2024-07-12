package server

import (
	"net/http"

	"github.com/gorilla/mux"
)

func (s *Server) RegisterRoutes() http.Handler {
	r := mux.NewRouter()

	r.HandleFunc("/ping", s.Ping).Methods("GET")
	r.HandleFunc("/health", s.healthHandler).Methods("GET")

	r.HandleFunc("/group/list", s.listGroupHandler).Methods("GET")
	r.HandleFunc("/group", s.createGroupHandler).Methods("POST")
	r.HandleFunc("/group", s.getGroupHandler).Methods("GET")

	r.HandleFunc("/images", s.getImageListHandler).Methods("GET")
	r.HandleFunc("/images", s.addImageListHandler).Methods("POST")
	// r.HandleFunc("/group", s.deleteGroupHandler).Methods("DELETE")

	return r
}
