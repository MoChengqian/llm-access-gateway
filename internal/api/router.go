package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/MoChengqian/llm-access-gateway/internal/api/handlers"
	"github.com/MoChengqian/llm-access-gateway/internal/auth"
	"github.com/MoChengqian/llm-access-gateway/internal/obs/tracing"
	"github.com/MoChengqian/llm-access-gateway/internal/service/chat"
	"github.com/MoChengqian/llm-access-gateway/internal/service/governance"
	"github.com/MoChengqian/llm-access-gateway/internal/service/models"
	usageservice "github.com/MoChengqian/llm-access-gateway/internal/service/usage"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"
)

type RequestMetricsRecorder interface {
	RecordHTTPRequest(method string, path string, status int, duration time.Duration)
	RecordReadyzFailure()
	RecordGovernanceRejection(reason string)
	RecordStreamRequest(ttft time.Duration)
	RecordStreamChunk()
}

type ProviderStatusMetricsRecorder interface {
	SyncProviderStatuses(statuses []handlers.ProviderBackendStatus)
}

func NewRouter(logger *zap.Logger, chatService chat.Service, modelsService models.Service, usageService usageservice.Service, authenticator auth.Authenticator, governanceService governance.Service, providers handlers.ProviderHealthReader, metricsHandler http.Handler, metricsRecorder RequestMetricsRecorder, maxRequestBodyBytes int64) http.Handler {
	r := chi.NewRouter()
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(chimiddleware.Recoverer)
	r.Use(requestIDHeader)
	r.Use(requestTracing(logger))
	r.Use(requestMetrics(metricsRecorder))
	r.Use(requestLogger(logger))

	healthHandler := handlers.NewHealthHandler(providers)
	chatHandler := handlers.NewChatHandler(chatService, governanceService, metricsRecorder)
	modelsHandler := handlers.NewModelsHandler(modelsService)
	usageHandler := handlers.NewUsageHandler(usageService)

	if syncer, ok := metricsRecorder.(ProviderStatusMetricsRecorder); ok && providers != nil {
		syncer.SyncProviderStatuses(providers.BackendStatuses())
	}

	r.Get("/healthz", healthHandler.Healthz)
	r.Get("/readyz", healthHandler.Readyz)
	r.Get("/debug/providers", healthHandler.Providers)
	if metricsHandler != nil {
		r.Get("/metrics", func(w http.ResponseWriter, r *http.Request) {
			metricsHandler.ServeHTTP(w, r)
		})
	}
	r.Get("/v1/models", requireAPIKey(authenticator, modelsHandler.List))
	r.Get("/v1/usage", requireAPIKey(authenticator, usageHandler.GetUsage))
	r.Post("/v1/chat/completions", chainHandler(
		requireAPIKey(authenticator, chatHandler.CreateCompletion),
		limitRequestBody(maxRequestBodyBytes),
	))

	return r
}

func chainHandler(handler http.HandlerFunc, middlewares ...func(http.Handler) http.Handler) http.HandlerFunc {
	var wrapped http.Handler = handler
	for idx := len(middlewares) - 1; idx >= 0; idx-- {
		if middlewares[idx] == nil {
			continue
		}
		wrapped = middlewares[idx](wrapped)
	}

	return func(w http.ResponseWriter, r *http.Request) {
		wrapped.ServeHTTP(w, r)
	}
}

func limitRequestBody(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if maxBytes <= 0 {
			return next
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			}
			next.ServeHTTP(w, r)
		})
	}
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

func requestTracing(logger *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := chimiddleware.NewWrapResponseWriter(w, r.ProtoMajor)
			ctx, span := tracing.StartRequestSpan(r.Context(), logger, chimiddleware.GetReqID(r.Context()), "http.request",
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
			)
			if traceID := tracing.TraceIDFromContext(ctx); traceID != "" {
				ww.Header().Set("X-Trace-Id", traceID)
			}

			next.ServeHTTP(ww, r.WithContext(ctx))

			span.End(nil,
				zap.Int("status", ww.Status()),
				zap.Int("bytes", ww.BytesWritten()),
			)
		})
	}
}

func requestLogger(logger *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := chimiddleware.NewWrapResponseWriter(w, r.ProtoMajor)

			next.ServeHTTP(ww, r)

			fields := []zap.Field{
				zap.String("request_id", chimiddleware.GetReqID(r.Context())),
				zap.String("trace_id", tracing.TraceIDFromContext(r.Context())),
				zap.String("span_id", tracing.SpanIDFromContext(r.Context())),
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
				zap.Int("status", ww.Status()),
				zap.Int("bytes", ww.BytesWritten()),
				zap.Duration("duration", time.Since(start)),
				zap.String("real_ip", r.RemoteAddr),
				zap.String("user_agent", r.UserAgent()),
				zap.String("content_type", ww.Header().Get("Content-Type")),
			}
			if principal, ok := auth.PrincipalFromContext(r.Context()); ok {
				fields = append(fields,
					zap.String("tenant_name", principal.Tenant.Name),
					zap.Int("tenant_id", int(principal.Tenant.ID)),
					zap.Int("api_key_id", int(principal.APIKeyID)),
					zap.String("api_key_prefix", principal.APIKeyPrefix),
				)
			}

			logger.Info(
				"http request completed",
				fields...,
			)
		})
	}
}

func requestMetrics(recorder RequestMetricsRecorder) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			startedAt := time.Now()
			ww := chimiddleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)

			if recorder == nil {
				return
			}

			recorder.RecordHTTPRequest(r.Method, r.URL.Path, ww.Status(), time.Since(startedAt))
			if r.URL.Path == "/readyz" && ww.Status() != http.StatusOK {
				recorder.RecordReadyzFailure()
			}
		})
	}
}
