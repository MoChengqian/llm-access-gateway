package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/MoChengqian/llm-access-gateway/internal/api/handlers"
	"github.com/MoChengqian/llm-access-gateway/internal/auth"
	"github.com/MoChengqian/llm-access-gateway/internal/service/chat"
	"github.com/MoChengqian/llm-access-gateway/internal/service/governance"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"
)

func NewRouter(logger *zap.Logger, chatService chat.Service, authenticator auth.Authenticator, governanceService governance.Service) http.Handler {
	r := chi.NewRouter()
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(chimiddleware.Recoverer)
	r.Use(requestIDHeader)
	r.Use(requestLogger(logger))

	healthHandler := handlers.NewHealthHandler()
	chatHandler := handlers.NewChatHandler(chatService, governanceService)

	r.Get("/healthz", healthHandler.Healthz)
	r.Get("/readyz", healthHandler.Readyz)
	r.Post("/v1/chat/completions", requireAPIKey(authenticator, chatHandler.CreateCompletion))

	return r
}

func requireAPIKey(authenticator auth.Authenticator, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticator.AuthenticateRequest(r.Context(), r.Header.Get("Authorization"))
		if err != nil {
			writeAuthError(w, err)
			return
		}

		next.ServeHTTP(w, r.WithContext(auth.WithPrincipal(r.Context(), principal)))
	}
}

func writeAuthError(w http.ResponseWriter, err error) {
	w.Header().Set("WWW-Authenticate", "Bearer")

	switch {
	case errors.Is(err, auth.ErrMissingAPIKey):
		handlers.WriteErrorJSON(w, http.StatusUnauthorized, "missing api key")
	case errors.Is(err, auth.ErrInvalidAPIKey):
		handlers.WriteErrorJSON(w, http.StatusUnauthorized, "invalid api key")
	case errors.Is(err, auth.ErrDisabledAPIKey):
		handlers.WriteErrorJSON(w, http.StatusUnauthorized, "disabled api key")
	default:
		handlers.WriteErrorJSON(w, http.StatusInternalServerError, "internal server error")
	}
}

func requestIDHeader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requestID := chimiddleware.GetReqID(r.Context()); requestID != "" {
			w.Header().Set("X-Request-Id", requestID)
		}
		next.ServeHTTP(w, r)
	})
}

func requestLogger(logger *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := chimiddleware.NewWrapResponseWriter(w, r.ProtoMajor)

			next.ServeHTTP(ww, r)

			logger.Info(
				"http request completed",
				zap.String("request_id", chimiddleware.GetReqID(r.Context())),
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
				zap.Int("status", ww.Status()),
				zap.Int("bytes", ww.BytesWritten()),
				zap.Duration("duration", time.Since(start)),
			)
		})
	}
}
