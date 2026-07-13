// This file is safe to edit. Once it exists it will not be overwritten

package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	stderrors "errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/go-openapi/errors"
	"github.com/go-openapi/runtime"

	"github.com/container-registry/harbor-satellite/internal/env"
	auditlog "github.com/container-registry/harbor-satellite/internal/groundcontrol/logger"
	gcmiddleware "github.com/container-registry/harbor-satellite/internal/groundcontrol/middleware"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/spiffe"
	swaggerhandlers "github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/handlers"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/server/operations"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/server/operations/auth"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/server/operations/configs"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/server/operations/groups"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/server/operations/health"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/server/operations/satellites"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/server/operations/spire"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/server/operations/users"
)

var runtimeResources struct {
	sync.Mutex
	prepared       bool
	certWatcher    *gcmiddleware.CertWatcher
	spiffeProvider spiffe.Provider
	spiffeTLS      *tls.Config
}

// PrepareRuntime initializes TLS resources that must report startup failures
// before listeners begin accepting requests. Static certificates get hot
// reload support; SPIFFE deployments get their Workload API-backed mTLS config.
func PrepareRuntime(configuredCertFile, configuredKeyFile string) error {
	runtimeResources.Lock()
	defer runtimeResources.Unlock()
	if runtimeResources.prepared {
		return nil
	}

	spiffeConfig := spiffe.LoadConfig()
	if spiffeConfig.Enabled {
		provider, err := spiffe.NewProvider(spiffeConfig)
		if err != nil {
			return fmt.Errorf("create SPIFFE provider: %w", err)
		}
		spiffeTLS, err := buildSPIFFETLSConfig(provider)
		if err != nil {
			_ = provider.Close()
			return err
		}
		runtimeResources.spiffeProvider = provider
		runtimeResources.spiffeTLS = spiffeTLS
		runtimeResources.prepared = true
		log.Printf("SPIFFE TLS enabled with trust domain %q", spiffeConfig.TrustDomain)
		return nil
	}

	certFile := firstNonEmpty(configuredCertFile, os.Getenv("TLS_CERTIFICATE"), env.GC.TLS.CertFile)
	keyFile := firstNonEmpty(configuredKeyFile, os.Getenv("TLS_PRIVATE_KEY"), env.GC.TLS.KeyFile)
	if certFile != "" && keyFile != "" {
		watcher, err := gcmiddleware.NewCertWatcher(certFile, keyFile)
		if err != nil {
			return fmt.Errorf("initialize TLS certificate watcher: %w", err)
		}
		watcher.Start(30 * time.Second)
		runtimeResources.certWatcher = watcher
		log.Println("Certificate watcher started for TLS hot-reload")
	}

	runtimeResources.prepared = true
	return nil
}

func configureFlags(api *operations.GroundControlAPI) {
	// api.CommandLineOptionsGroups = []cmdutils.CommandLineOptionsGroup{ ... }
	_ = api
}

func configureAPI(api *operations.GroundControlAPI) http.Handler {
	api.ServeError = errors.ServeError
	api.UseSwaggerUI()

	api.JSONConsumer = runtime.JSONConsumer()
	api.JSONProducer = runtime.JSONProducer()
	api.TxtProducer = runtime.TextProducer()

	api.BearerAuthAuth = swaggerhandlers.AuthenticateBearer

	api.GroupsAddSatelliteToGroupHandler = groups.AddSatelliteToGroupHandlerFunc(swaggerhandlers.AddSatelliteToGroup)
	api.UsersChangeOwnPasswordHandler = users.ChangeOwnPasswordHandlerFunc(swaggerhandlers.ChangeOwnPassword)
	api.UsersChangeUserPasswordHandler = users.ChangeUserPasswordHandlerFunc(swaggerhandlers.ChangeUserPassword)
	api.ConfigsCreateConfigHandler = configs.CreateConfigHandlerFunc(swaggerhandlers.CreateConfig)
	api.UsersCreateUserHandler = users.CreateUserHandlerFunc(swaggerhandlers.CreateUser)
	api.ConfigsDeleteConfigHandler = configs.DeleteConfigHandlerFunc(swaggerhandlers.DeleteConfig)
	api.GroupsDeleteGroupHandler = groups.DeleteGroupHandlerFunc(swaggerhandlers.DeleteGroup)
	api.SatellitesDeleteSatelliteHandler = satellites.DeleteSatelliteHandlerFunc(swaggerhandlers.DeleteSatellite)
	api.UsersDeleteUserHandler = users.DeleteUserHandlerFunc(swaggerhandlers.DeleteUser)
	api.SatellitesGetCachedImagesHandler = satellites.GetCachedImagesHandlerFunc(swaggerhandlers.GetCachedImages)
	api.ConfigsGetConfigHandler = configs.GetConfigHandlerFunc(swaggerhandlers.GetConfig)
	api.GroupsGetGroupHandler = groups.GetGroupHandlerFunc(swaggerhandlers.GetGroup)
	api.SatellitesGetSatelliteHandler = satellites.GetSatelliteHandlerFunc(swaggerhandlers.GetSatellite)
	api.SatellitesGetSatelliteStatusHandler = satellites.GetSatelliteStatusHandlerFunc(swaggerhandlers.GetSatelliteStatus)
	api.SpireGetSpireStatusHandler = spire.GetSpireStatusHandlerFunc(swaggerhandlers.GetSpireStatus)
	api.UsersGetUserHandler = users.GetUserHandlerFunc(swaggerhandlers.GetUser)
	api.HealthHealthHandler = health.HealthHandlerFunc(swaggerhandlers.Health)
	api.SatellitesListActiveSatellitesHandler = satellites.ListActiveSatellitesHandlerFunc(swaggerhandlers.ListActiveSatellites)
	api.ConfigsListConfigsHandler = configs.ListConfigsHandlerFunc(swaggerhandlers.ListConfigs)
	api.GroupsListGroupSatellitesHandler = groups.ListGroupSatellitesHandlerFunc(swaggerhandlers.ListGroupSatellites)
	api.GroupsListGroupsHandler = groups.ListGroupsHandlerFunc(swaggerhandlers.ListGroups)
	api.SatellitesListSatellitesHandler = satellites.ListSatellitesHandlerFunc(swaggerhandlers.ListSatellites)
	api.SpireListSpireAgentsHandler = spire.ListSpireAgentsHandlerFunc(swaggerhandlers.ListSpireAgents)
	api.SatellitesListStaleSatellitesHandler = satellites.ListStaleSatellitesHandlerFunc(swaggerhandlers.ListStaleSatellites)
	api.UsersListUsersHandler = users.ListUsersHandlerFunc(swaggerhandlers.ListUsers)
	api.AuthLoginHandler = auth.LoginHandlerFunc(swaggerhandlers.Login)
	api.AuthLogoutHandler = auth.LogoutHandlerFunc(swaggerhandlers.Logout)
	api.HealthPingHandler = health.PingHandlerFunc(swaggerhandlers.Ping)
	api.SatellitesRegisterSatelliteHandler = satellites.RegisterSatelliteHandlerFunc(swaggerhandlers.RegisterSatellite)
	api.SpireRegisterSatelliteWithSpiffeHandler = spire.RegisterSatelliteWithSpiffeHandlerFunc(swaggerhandlers.RegisterSatelliteWithSpiffe)
	api.GroupsRemoveSatelliteFromGroupHandler = groups.RemoveSatelliteFromGroupHandlerFunc(swaggerhandlers.RemoveSatelliteFromGroup)
	api.ConfigsSetSatelliteConfigHandler = configs.SetSatelliteConfigHandlerFunc(swaggerhandlers.SetSatelliteConfig)
	api.SatellitesSpiffeZtrHandler = satellites.SpiffeZtrHandlerFunc(swaggerhandlers.SpiffeZtr)
	api.GroupsSyncGroupHandler = groups.SyncGroupHandlerFunc(swaggerhandlers.SyncGroup)
	api.SatellitesSyncSatelliteHandler = satellites.SyncSatelliteHandlerFunc(swaggerhandlers.SyncSatellite)
	api.ConfigsUpdateConfigHandler = configs.UpdateConfigHandlerFunc(swaggerhandlers.UpdateConfig)
	api.SatellitesZtrHandler = satellites.ZtrHandlerFunc(swaggerhandlers.Ztr)

	api.AddMiddlewareFor(http.MethodPost, "/login", swaggerhandlers.RateLimitMiddleware(
		auditlog.OpLogin, auditlog.ResSession, "login",
	))
	api.AddMiddlewareFor(http.MethodGet, "/satellites/ztr/{token}", swaggerhandlers.RateLimitMiddleware(
		auditlog.OpAuth, auditlog.ResSatellite, "ztr",
	))
	api.AddMiddlewareFor(http.MethodGet, "/satellites/spiffe-ztr", swaggerhandlers.RateLimitMiddleware(
		auditlog.OpAuth, auditlog.ResSatellite, "spiffe_ztr",
	))
	// Extract the optional SVID here; the operation handler enforces its
	// presence so rejected requests also receive a structured audit event.
	api.AddMiddlewareFor(http.MethodGet, "/satellites/spiffe-ztr", spiffe.AuthMiddleware)
	api.AddMiddlewareFor(http.MethodPost, "/satellites/sync", spiffe.AuthMiddleware)
	api.AddMiddlewareFor(http.MethodPatch, "/api/configs/{config}", swaggerhandlers.CaptureConfigPatchBody)

	api.PreServerShutdown = swaggerhandlers.StopBackgroundJobs
	api.ServerShutdown = func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := ShutdownApplication(ctx); err != nil {
			log.Printf("Ground Control resource shutdown error: %v", err)
		}
	}

	return setupGlobalMiddleware(api.Serve(setupMiddlewares))
}

func configureTLS(tlsConfig *tls.Config) {
	runtimeResources.Lock()
	spiffeTLS := runtimeResources.spiffeTLS
	certWatcher := runtimeResources.certWatcher
	runtimeResources.Unlock()

	switch {
	case spiffeTLS != nil:
		configured := spiffeTLS.Clone()
		if configured.MinVersion == 0 {
			configured.MinVersion = tls.VersionTLS12
		}
		if len(configured.NextProtos) == 0 {
			configured.NextProtos = []string{"h2", "http/1.1"}
		}
		*tlsConfig = *configured
	case certWatcher != nil:
		tlsConfig.Certificates = nil
		tlsConfig.GetCertificate = certWatcher.GetCertificate
	}
}

func configureServer(server *http.Server, scheme, addr string) {
	server.IdleTimeout = time.Minute
	server.ReadTimeout = 10 * time.Second
	server.ReadHeaderTimeout = 10 * time.Second
	server.WriteTimeout = 30 * time.Second
	_ = scheme
	_ = addr
}

func setupMiddlewares(handler http.Handler) http.Handler {
	return handler
}

func setupGlobalMiddleware(handler http.Handler) http.Handler {
	return swaggerhandlers.RequestIDMiddleware(handler)
}

// ShutdownApplication releases resources owned outside the generated HTTP
// servers. It is called by the generated shutdown hook and by main's fallback
// defer for startup/listener failures.
func ShutdownApplication(ctx context.Context) error {
	handlerErr := swaggerhandlers.Shutdown(ctx)

	runtimeResources.Lock()
	certWatcher := runtimeResources.certWatcher
	provider := runtimeResources.spiffeProvider
	runtimeResources.certWatcher = nil
	runtimeResources.spiffeProvider = nil
	runtimeResources.spiffeTLS = nil
	runtimeResources.prepared = false
	runtimeResources.Unlock()

	if certWatcher != nil {
		certWatcher.Stop()
	}
	var providerErr error
	if provider != nil {
		providerErr = provider.Close()
	}
	return stderrors.Join(handlerErr, providerErr)
}

func buildSPIFFETLSConfig(provider spiffe.Provider) (*tls.Config, error) {
	authorizer := spiffe.NewSatelliteAuthorizer(provider.GetTrustDomain())
	tlsConfig, err := provider.GetTLSConfig(context.Background(), authorizer.AuthorizeID())
	if err != nil {
		return nil, fmt.Errorf("build SPIFFE TLS config: %w", err)
	}

	// Public endpoints must remain reachable without an SVID. When a client
	// certificate is present, go-spiffe's verifier still authenticates it.
	tlsConfig.ClientAuth = tls.RequestClientCert
	originalVerify := tlsConfig.VerifyPeerCertificate
	if originalVerify != nil {
		tlsConfig.VerifyPeerCertificate = func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return nil
			}
			return originalVerify(rawCerts, verifiedChains)
		}
	}

	originalGetConfig := tlsConfig.GetConfigForClient
	if originalGetConfig != nil {
		tlsConfig.GetConfigForClient = func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
			config, err := originalGetConfig(hello)
			if err != nil || config == nil {
				return config, err
			}
			config.ClientAuth = tls.RequestClientCert
			innerVerify := config.VerifyPeerCertificate
			if innerVerify != nil {
				config.VerifyPeerCertificate = func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
					if len(rawCerts) == 0 {
						return nil
					}
					return innerVerify(rawCerts, verifiedChains)
				}
			}
			return config, nil
		}
	}

	return tlsConfig, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
