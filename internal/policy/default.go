package policy

import (
	_ "embed"
	"fmt"
)

// DefaultCompanyID names the embedded default policy in logs.
const DefaultCompanyID = "_default"

// defaultJSON is the policy for a company that has none (D-042, D-063): no
// money limit (it would decline every other currency, D-041), gambling,
// quasi-cash and money transfers blocked, and a fail-closed fallback. A unit
// test validates it, so a broken default fails the build, not production.
//
//go:embed default_policy.json
var defaultJSON []byte

var defaultDoc = mustLoadDefault()

// GetDefault returns the embedded default policy, already validated.
func GetDefault() Document {
	return defaultDoc
}

func mustLoadDefault() Document {
	doc, err := Load(defaultJSON)
	if err != nil {
		panic(fmt.Sprintf("embedded default policy is invalid: %v", err))
	}

	return doc
}
