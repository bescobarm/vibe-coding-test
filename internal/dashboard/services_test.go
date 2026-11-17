package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

var errDockerDown = errors.New("docker is not running")

// fakeServices keeps one running flag per service, like a tiny Docker.
type fakeServices struct {
	running   map[string]bool
	changeErr error
	statusErr error
}

func (f *fakeServices) IsRunning(_ context.Context, service string) (bool, error) {
	return f.running[service], f.statusErr
}

func (f *fakeServices) Stop(_ context.Context, service string) error {
	if f.changeErr != nil {
		return f.changeErr
	}

	f.running[service] = false

	return nil
}

func (f *fakeServices) Start(_ context.Context, service string) error {
	if f.changeErr != nil {
		return f.changeErr
	}

	f.running[service] = true

	return nil
}

func newServicesServer(t *testing.T, services Services) http.Handler {
	t.Helper()
	server, err := NewServer(Config{
		Engine: &fakeEngine{}, Cards: fakeCards{}, Services: services, Client: http.DefaultClient,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	require.NoError(t, err)
	return server.Handler()
}

func decodeStatus(c *require.Assertions, recorder *httptest.ResponseRecorder) ServiceStatus {
	var status ServiceStatus
	c.NoError(json.Unmarshal(recorder.Body.Bytes(), &status))
	return status
}

func TestThePageOffersToStopRedis(t *testing.T) {
	c := require.New(t)

	recorder := doRequest(newTestServer(t, &fakeEngine{}, ""), http.MethodGet, "/", "")

	c.Contains(recorder.Body.String(), `id="redisToggle"`)
}

func TestARunningServiceIsReported(t *testing.T) {
	c := require.New(t)
	handler := newServicesServer(t, &fakeServices{running: map[string]bool{"redis": true}})

	recorder := doRequest(handler, http.MethodGet, "/api/services/redis", "")

	c.Equal(http.StatusOK, recorder.Code)
	c.Equal(ServiceStatus{Service: "redis", Running: true}, decodeStatus(c, recorder))
}

func TestRedisIsStopped(t *testing.T) {
	c := require.New(t)
	services := &fakeServices{running: map[string]bool{"redis": true}}

	recorder := doRequest(newServicesServer(t, services), http.MethodPost, "/api/services/redis/stop", "")

	c.Equal(http.StatusOK, recorder.Code)
	c.Equal(ServiceStatus{Service: "redis", Running: false}, decodeStatus(c, recorder))
	c.False(services.running["redis"])
}

func TestRedisIsStartedAgain(t *testing.T) {
	c := require.New(t)
	services := &fakeServices{running: map[string]bool{"redis": false}}

	recorder := doRequest(newServicesServer(t, services), http.MethodPost, "/api/services/redis/start", "")

	c.Equal(http.StatusOK, recorder.Code)
	c.Equal(ServiceStatus{Service: "redis", Running: true}, decodeStatus(c, recorder))
}

// Postgres is not on the list: the dashboard needs it, and the page must not
// be able to stop any container it names.
func TestOnlyRedisCanBeStopped(t *testing.T) {
	c := require.New(t)
	services := &fakeServices{running: map[string]bool{"postgres": true}}

	recorder := doRequest(newServicesServer(t, services), http.MethodPost, "/api/services/postgres/stop", "")

	c.Equal(http.StatusNotFound, recorder.Code)
	c.Equal(`unknown service: "postgres"`, decodeError(c, recorder))
	c.True(services.running["postgres"])
}

func TestAnUnknownServiceHasNoStatus(t *testing.T) {
	c := require.New(t)

	recorder := doRequest(newServicesServer(t, &fakeServices{}), http.MethodGet, "/api/services/postgres", "")

	c.Equal(http.StatusNotFound, recorder.Code)
}

func TestAnUnknownActionIsRejected(t *testing.T) {
	c := require.New(t)
	services := &fakeServices{running: map[string]bool{"redis": true}}

	recorder := doRequest(newServicesServer(t, services), http.MethodPost, "/api/services/redis/restart", "")

	c.Equal(http.StatusBadRequest, recorder.Code)
	c.Equal(errUnknownAction.Error(), decodeError(c, recorder))
	c.True(services.running["redis"])
}

func TestAFailedStopIsReported(t *testing.T) {
	c := require.New(t)
	services := &fakeServices{running: map[string]bool{"redis": true}, changeErr: errDockerDown}

	recorder := doRequest(newServicesServer(t, services), http.MethodPost, "/api/services/redis/stop", "")

	c.Equal(http.StatusBadGateway, recorder.Code)
	c.Equal(errDockerDown.Error(), decodeError(c, recorder))
}

func TestAFailedStartIsReported(t *testing.T) {
	c := require.New(t)
	services := &fakeServices{running: map[string]bool{"redis": false}, changeErr: errDockerDown}

	recorder := doRequest(newServicesServer(t, services), http.MethodPost, "/api/services/redis/start", "")

	c.Equal(http.StatusBadGateway, recorder.Code)
}

func TestAFailedStatusIsReported(t *testing.T) {
	c := require.New(t)
	services := &fakeServices{statusErr: errDockerDown}

	recorder := doRequest(newServicesServer(t, services), http.MethodGet, "/api/services/redis", "")

	c.Equal(http.StatusBadGateway, recorder.Code)
	c.Equal(errDockerDown.Error(), decodeError(c, recorder))
}

func TestAStopReportsAFailedStatus(t *testing.T) {
	c := require.New(t)
	services := &fakeServices{running: map[string]bool{"redis": true}, statusErr: errDockerDown}

	recorder := doRequest(newServicesServer(t, services), http.MethodPost, "/api/services/redis/stop", "")

	c.Equal(http.StatusBadGateway, recorder.Code)
}

// Without a controller (e.g. the engine runs elsewhere), the page gets a clear answer.
func TestServicesCanBeTurnedOff(t *testing.T) {
	c := require.New(t)

	recorder := doRequest(newTestServer(t, &fakeEngine{}, ""), http.MethodGet, "/api/services/redis", "")

	c.Equal(http.StatusNotImplemented, recorder.Code)
	c.Equal(errNoServices.Error(), decodeError(c, recorder))
}

// A folder with no compose file makes docker compose fail, whether or not Docker is running.
func TestComposeReportsAFailedCommand(t *testing.T) {
	c := require.New(t)
	services := ComposeServices{Dir: t.TempDir()}

	_, err := services.IsRunning(t.Context(), "redis")
	c.ErrorIs(err, errServiceControl)

	c.ErrorIs(services.Stop(t.Context(), "redis"), errServiceControl)
	c.ErrorIs(services.Start(t.Context(), "redis"), errServiceControl)
}
