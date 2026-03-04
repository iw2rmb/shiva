package httpserver

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/gofiber/fiber/v2/middleware/recover"

	"github.com/iw2rmb/shiva/internal/config"
	"github.com/iw2rmb/shiva/internal/observability"
	"github.com/iw2rmb/shiva/internal/store"
	"go.opentelemetry.io/otel/trace"
)

type Server struct {
	app            *fiber.App
	cfg            config.Config
	logger         *slog.Logger
	store          *store.Store
	gitlabIngestor gitlabWebhookIngestor
	readStore      readRouteStore
	metrics        *observability.Metrics
	tracer         trace.Tracer
}

const (
	defaultIngressBodyLimitBytes = 1024 * 1024
	defaultIngressRateLimitMax   = 120
	defaultIngressRateLimit      = 60 * time.Second
	defaultMetricsPath           = "/internal/metrics"
)

type Option func(*Server)

func WithTelemetry(telemetry *observability.Telemetry) Option {
	return func(s *Server) {
		if telemetry == nil {
			return
		}
		s.metrics = telemetry.Metrics()
		s.tracer = telemetry.Tracer()
	}
}

func WithGitLabWebhookIngestor(ingestor gitlabWebhookIngestor) Option {
	return func(s *Server) {
		if ingestor != nil {
			s.gitlabIngestor = ingestor
		}
	}
}

func New(cfg config.Config, logger *slog.Logger, store *store.Store, options ...Option) *Server {
	cfg = normalizeServerConfig(cfg)

	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		BodyLimit:             cfg.IngressBodyLimit,
	})
	app.Use(recover.New())
	app.Use(requestIDMiddleware())

	srv := &Server{
		app:            app,
		cfg:            cfg,
		logger:         logger,
		store:          store,
		gitlabIngestor: store,
		readStore:      store,
		tracer:         trace.NewNoopTracerProvider().Tracer("github.com/iw2rmb/shiva"),
	}
	for _, option := range options {
		option(srv)
	}
	if srv.tracer == nil {
		srv.tracer = trace.NewNoopTracerProvider().Tracer("github.com/iw2rmb/shiva")
	}
	srv.registerRoutes()
	return srv
}

func (s *Server) Start() error {
	return s.app.Listen(s.cfg.HTTPAddr)
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.app.ShutdownWithContext(ctx)
}

func (s *Server) registerRoutes() {
	s.app.Get("/healthz", s.healthz)

	metricsPath := strings.TrimSpace(s.cfg.MetricsPath)
	if metricsPath != "" && s.metrics != nil {
		s.app.Get(metricsPath, adaptor.HTTPHandler(s.metrics.Handler()))
	}

	webhookGroup := s.app.Group("/internal/webhooks")
	webhookGroup.Use(limiter.New(limiter.Config{
		Max:        s.cfg.IngressRateLimitMax,
		Expiration: s.cfg.IngressRateLimit,
		LimitReached: func(c *fiber.Ctx) error {
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"error": "ingress rate limit exceeded",
			})
		},
	}))
	webhookGroup.Post("/gitlab", s.handleGitLabWebhook)

	// Selector-aware operation routes must be registered before no-selector variants.
	for _, method := range readOperationHTTPMethods {
		s.app.Add(method, "/:tenant/:repo/:selector/*", s.handleOperationBySelector)
	}
	s.app.Get("/:tenant/:repo/:selector.:format", s.handleGetSpecBySelector)
	s.app.Get("/:tenant/:repo.:format", s.handleGetSpecNoSelector)
	for _, method := range readOperationHTTPMethods {
		s.app.Add(method, "/:tenant/:repo/*", s.handleOperationNoSelector)
	}
}

type healthResponse struct {
	Status  string       `json:"status"`
	Store   store.Health `json:"store"`
	Service string       `json:"service"`
}

func (s *Server) healthz(c *fiber.Ctx) error {
	return c.Status(fiber.StatusOK).JSON(healthResponse{
		Status:  "ok",
		Service: "shiva",
		Store:   s.store.Health(c.Context()),
	})
}

func (s *Server) App() *fiber.App {
	return s.app
}

func normalizeServerConfig(cfg config.Config) config.Config {
	if cfg.IngressBodyLimit < 1 {
		cfg.IngressBodyLimit = defaultIngressBodyLimitBytes
	}
	if cfg.IngressRateLimitMax < 1 {
		cfg.IngressRateLimitMax = defaultIngressRateLimitMax
	}
	if cfg.IngressRateLimit <= 0 {
		cfg.IngressRateLimit = defaultIngressRateLimit
	}
	if strings.TrimSpace(cfg.MetricsPath) == "" {
		cfg.MetricsPath = defaultMetricsPath
	}
	return cfg
}
