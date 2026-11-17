package dashboard

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"slices"
	"strings"
)

const (
	serviceRedis  = "redis"
	actionStop    = "stop"
	actionStart   = "start"
	dockerCommand = "docker"
	composeArg    = "compose"
)

// Errors shown to the page.
var (
	errUnknownService = errors.New("unknown service")
	errUnknownAction  = errors.New("action must be stop or start")
	errNoServices     = errors.New("this dashboard can't control services")
	errServiceControl = errors.New("docker compose failed")
)

// Services stops and starts a dependency of the engine, to show its fallback
// live. The page can only reach the services in controllableServices.
type Services interface {
	IsRunning(ctx context.Context, service string) (bool, error)
	Stop(ctx context.Context, service string) error
	Start(ctx context.Context, service string) error
}

// controllableServices is every service the page may stop. Postgres is left
// out: the dashboard itself needs it to create scenario cards.
var controllableServices = []string{serviceRedis}

// ComposeServices controls the containers of the docker-compose.yml in Dir.
type ComposeServices struct {
	Dir string
}

// IsRunning implements Services.
func (c ComposeServices) IsRunning(ctx context.Context, service string) (bool, error) {
	output, err := c.runCompose(ctx, "ps", "--services", "--status", "running")
	if err != nil {
		return false, err
	}

	return slices.Contains(strings.Fields(output), service), nil
}

// Stop implements Services.
func (c ComposeServices) Stop(ctx context.Context, service string) error {
	_, err := c.runCompose(ctx, actionStop, service)

	return err
}

// Start implements Services. It returns once the service is healthy.
func (c ComposeServices) Start(ctx context.Context, service string) error {
	_, err := c.runCompose(ctx, "up", "-d", "--wait", service)

	return err
}

func (c ComposeServices) runCompose(ctx context.Context, args ...string) (string, error) {
	command := exec.CommandContext(ctx, dockerCommand, append([]string{composeArg}, args...)...)
	command.Dir = c.Dir

	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w: %w: %s", errServiceControl, err, strings.TrimSpace(string(output)))
	}

	return string(output), nil
}

// ServiceStatus is what the page shows for one service.
type ServiceStatus struct {
	Service string `json:"service"`
	Running bool   `json:"running"`
}

// getService reports whether a service is running.
func (s *Server) getService(w http.ResponseWriter, r *http.Request) {
	service, err := s.findService(r)
	if err != nil {
		s.writeServiceError(w, err)
		return
	}

	s.writeServiceStatus(w, r, service)
}

// changeService stops or starts a service, then reports its new status.
func (s *Server) changeService(w http.ResponseWriter, r *http.Request) {
	service, err := s.findService(r)
	if err != nil {
		s.writeServiceError(w, err)
		return
	}

	switch r.PathValue("action") {
	case actionStop:
		err = s.config.Services.Stop(r.Context(), service)
	case actionStart:
		err = s.config.Services.Start(r.Context(), service)
	default:
		err = errUnknownAction
	}

	if err != nil {
		s.writeServiceError(w, err)
		return
	}

	s.writeServiceStatus(w, r, service)
}

func (s *Server) findService(r *http.Request) (string, error) {
	if s.config.Services == nil {
		return "", errNoServices
	}

	service := r.PathValue("service")
	if !slices.Contains(controllableServices, service) {
		return "", fmt.Errorf("%w: %q", errUnknownService, service)
	}

	return service, nil
}

func (s *Server) writeServiceStatus(w http.ResponseWriter, r *http.Request, service string) {
	running, err := s.config.Services.IsRunning(r.Context(), service)
	if err != nil {
		s.writeServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, ServiceStatus{Service: service, Running: running})
}

func (s *Server) writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errUnknownService):
		s.writeError(w, http.StatusNotFound, err)
	case errors.Is(err, errUnknownAction):
		s.writeError(w, http.StatusBadRequest, err)
	case errors.Is(err, errNoServices):
		s.writeError(w, http.StatusNotImplemented, err)
	default:
		s.writeError(w, http.StatusBadGateway, err)
	}
}
