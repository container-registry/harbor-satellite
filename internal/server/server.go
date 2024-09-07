package server

import (
	"context"
	"net/http"
)

type RouteRegistrar interface {
	RegisterRoutes(router Router)
}

// App struct with middleware support
type App struct {
	router     Router
	registrars []RouteRegistrar
	server     *http.Server
}

func NewApp(router Router, registrars ...RouteRegistrar) *App {
	return &App{
		router:     router,
		registrars: registrars,
		server:     &http.Server{Addr: ":9090", Handler: router},
	}
}

func (a *App) SetupRoutes() {
	for _, registrar := range a.registrars {
		registrar.RegisterRoutes(a.router)
	}
}

func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.router.ServeHTTP(w, r)
}
func (a *App) Start() error {
	return a.server.ListenAndServe()
}

func (a *App) Shutdown(ctx context.Context) error {
	return a.server.Shutdown(ctx)
}
