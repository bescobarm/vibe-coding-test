// Package testenv decides what an integration test does when Postgres or
// Redis is not running: skip it locally, or fail it under `make test` and
// `make cover`, where a skipped test would look like a pass (I-028).
package testenv

import (
	"os"
	"testing"
)

// RequireServicesEnv makes a missing service fail the test instead of skipping it.
const RequireServicesEnv = "TEST_REQUIRE_SERVICES"

const envTrue = "true"

// SkipOrFail stops the test because service is unreachable.
func SkipOrFail(t testing.TB, service string, err error) {
	t.Helper()

	if os.Getenv(RequireServicesEnv) == envTrue {
		t.Fatalf("%s not reachable and %s=true: %v", service, RequireServicesEnv, err)
	}

	t.Skipf("%s not reachable (run `make up`): %v", service, err)
}
