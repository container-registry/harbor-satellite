package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
)

type RouteRegistrar interface {
	RegisterRoutes(router Router)
}

// App struct with middleware support
type App struct {
	router     Router
	registrars []RouteRegistrar
	server     *http.Server
	ctx        context.Context
	Logger     *zerolog.Logger
}

func NewApp(router Router, ctx context.Context, logger *zerolog.Logger, registrars ...RouteRegistrar) *App {
	return &App{
		router:     router,
		registrars: registrars,
		ctx:        ctx,
		Logger:     logger,
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

func (a *App) SetupServer(g *errgroup.Group) {
	g.Go(func() error {
		a.Logger.Info().Msg("Starting server on :9090")
		if err := a.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	})
	g.Go(func() error {
		<-a.ctx.Done()
		a.Logger.Warn().Msg("Shutting down server")
		err := a.Shutdown(a.ctx)
		if err != nil {
			return fmt.Errorf("error shutting down server: %w", err)
		}
		return nil
	})
}
