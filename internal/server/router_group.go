package server

import "net/http"

// RouterGroup represents a group of routes with a common prefix and middleware
type RouterGroup struct {
	prefix     string
	middleware []Middleware
	router     Router
}

func (rg *RouterGroup) Use(middleware ...Middleware) {
	rg.middleware = append(rg.middleware, middleware...)
}

func (rg *RouterGroup) Handle(pattern string, handler http.Handler) {
	fullPattern := rg.prefix + pattern
	wrappedHandler := handler
	for i := len(rg.middleware) - 1; i >= 0; i-- {
		wrappedHandler = rg.middleware[i](wrappedHandler)
	}
	rg.router.Handle(fullPattern, wrappedHandler)
}

func (rg *RouterGroup) HandleFunc(pattern string, handlerFunc func(http.ResponseWriter, *http.Request)) {
	rg.Handle(pattern, http.HandlerFunc(handlerFunc))
}

func (rg *RouterGroup) Group(prefix string) *RouterGroup {
	return &RouterGroup{
		prefix:     rg.prefix + prefix,
		middleware: append([]Middleware{}, rg.middleware...),
		router:     rg.router,
	}
}
