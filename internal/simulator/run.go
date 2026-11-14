package simulator

import (
	"crypto/rand"
	_ "embed"
	"encoding/binary"
	"encoding/hex"
)

// DefaultPolicy is the simulated company's policy: the demo controls,
// card-testing and daily rules, risk thresholds and a fail-open fallback.
//
//go:embed policy.json
var DefaultPolicy []byte

const (
	runIDBytes = 3
	seedBytes  = 8
)

// NewRunID returns a short random ID that keeps a run's companies, cards and
// authorizations apart from every other run's.
func NewRunID() (string, error) {
	random := make([]byte, runIDBytes)

	_, err := rand.Read(random)
	if err != nil {
		return "", err
	}

	return hex.EncodeToString(random), nil
}

// PickSeed keeps a seed that was given, or draws a new one, so each run plays
// different cases (D-066). Reports print it to replay the run.
func PickSeed(given uint64) (uint64, error) {
	if given != 0 {
		return given, nil
	}

	random := make([]byte, seedBytes)

	_, err := rand.Read(random)
	if err != nil {
		return 0, err
	}

	return binary.BigEndian.Uint64(random), nil
}
