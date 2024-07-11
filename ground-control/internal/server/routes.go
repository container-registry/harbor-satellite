package server

import (
	"net/http"

	"github.com/gorilla/mux"
)

func (s *Server) RegisterRoutes() http.Handler {
	r := mux.NewRouter()

	r.HandleFunc("/", s.HelloWorldHandler).Methods("GET")
	r.HandleFunc("/health", s.healthHandler).Methods("GET")
	r.HandleFunc("/group", s.createGroupHandler).Methods("POST")

	return r
}
