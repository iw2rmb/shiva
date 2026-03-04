package httpserver

import (
	"context"
	"log/slog"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"

	"github.com/iw2rmb/shiva/internal/config"
	"github.com/iw2rmb/shiva/internal/store"
)

type Server struct {
	app            *fiber.App
	cfg            config.Config
	logger         *slog.Logger
	store          *store.Store
	gitlabIngestor gitlabWebhookIngestor
	readStore      readRouteStore
}

func New(cfg config.Config, logger *slog.Logger, store *store.Store) *Server {
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	app.Use(recover.New())

	srv := &Server{
		app:            app,
		cfg:            cfg,
		logger:         logger,
		store:          store,
		gitlabIngestor: store,
		readStore:      store,
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
	s.app.Post("/internal/webhooks/gitlab", s.handleGitLabWebhook)
	s.app.Get("/:tenant/:repo/:selector/spec.json", s.handleGetSpecJSON)
	s.app.Get("/:tenant/:repo/:selector/spec.yaml", s.handleGetSpecYAML)
	s.app.Get("/:tenant/:repo/:selector/endpoints", s.handleListEndpointsBySelector)
	s.app.Get("/:tenant/:repo/:selector/endpoints/:method/*", s.handleGetEndpointBySelector)
	s.app.Get("/:tenant/:repo/endpoints", s.handleListEndpointsNoSelector)
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
