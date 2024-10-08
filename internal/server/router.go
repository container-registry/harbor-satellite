package server

import "net/http"

type Router interface {
	// Handle registers a new route with the given pattern and handler on the router mux
	Handle(pattern string, handler http.Handler)
	// HandleFunc registers a new route with the given pattern and handler function on the router mux
	HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request))
	// ServeHTTP would dispatch the request to the default mux
	ServeHTTP(w http.ResponseWriter, r *http.Request)
	// Use adds middleware to the default router
	Use(middleware ...Middleware)
	// Group called on the router would create a group with the given prefix
	//and would inherit the middleware from the router and would be added to the root group of the router
	Group(prefix string) *RouterGroup
}

type Endpoint struct {
	Method string
	Path   string
}

// DefaultRouter
type DefaultRouter struct {
	// mux is the default http.ServeMux
	mux *http.ServeMux
	// middleware is the list of middleware to be applied to default router and all groups inside it
	middleware []Middleware
	// rootGroup is the root RouterGroup
	rootGroup *RouterGroup
	// endpoints is the list of all registered endpoints
	Endpoints []Endpoint
}

// NewDefaultRouter creates a new DefaultRouter with the given prefix
func NewDefaultRouter(prefix string) *DefaultRouter {
	dr := &DefaultRouter{mux: http.NewServeMux()}
	dr.rootGroup = &RouterGroup{prefix: prefix, router: dr}
	return dr
}

func (r *DefaultRouter) Handle(pattern string, handler http.Handler) {
	r.mux.Handle(pattern, handler)
}

func (r *DefaultRouter) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mux.ServeHTTP(w, req)
}

func (dr *DefaultRouter) HandleFunc(pattern string, handlerFunc func(http.ResponseWriter, *http.Request)) {
	dr.Handle(pattern, http.HandlerFunc(handlerFunc))
}

func (dr *DefaultRouter) Use(middleware ...Middleware) {
	dr.middleware = append(dr.middleware, middleware...)
}

// Group creates a new RouterGroup under the rootGroup with the given prefix
func (dr *DefaultRouter) Group(prefix string) *RouterGroup {
	return dr.rootGroup.Group(prefix)
}
