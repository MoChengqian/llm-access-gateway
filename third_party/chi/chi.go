package chi

import "net/http"

type Middleware func(http.Handler) http.Handler

type Router struct {
	middlewares []Middleware
	routes      []route
}

type route struct {
	method  string
	pattern string
	handler http.Handler
}

func NewRouter() *Router {
	return &Router{}
}

func (r *Router) Use(middlewares ...func(http.Handler) http.Handler) {
	for _, middleware := range middlewares {
		r.middlewares = append(r.middlewares, Middleware(middleware))
	}
}

func (r *Router) Get(pattern string, handlerFn http.HandlerFunc) {
	r.handle(http.MethodGet, pattern, handlerFn)
}

func (r *Router) Post(pattern string, handlerFn http.HandlerFunc) {
	r.handle(http.MethodPost, pattern, handlerFn)
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	var pathMatched bool

	for _, route := range r.routes {
		if route.pattern != req.URL.Path {
			continue
		}

		pathMatched = true
		if route.method != req.Method {
			continue
		}

		handler := route.handler
		for idx := len(r.middlewares) - 1; idx >= 0; idx-- {
			handler = r.middlewares[idx](handler)
		}

		handler.ServeHTTP(w, req)
		return
	}

	if pathMatched {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	http.NotFound(w, req)
}

func (r *Router) handle(method, pattern string, handlerFn http.HandlerFunc) {
	r.routes = append(r.routes, route{
		method:  method,
		pattern: pattern,
		handler: handlerFn,
	})
}
