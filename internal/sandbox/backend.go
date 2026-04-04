package sandbox

import (
	"context"
	"fmt"
)

// Backend is the interface that all sandbox backends implement.
type Backend interface {
	Run(ctx context.Context, cfg RunConfig) (Result, error)
}

// BackendType names a backend implementation.
type BackendType string

const (
	BackendAuto      BackendType = "auto"
	BackendRlimits   BackendType = "rlimits"
	BackendDynamoRIO BackendType = "dynamorio"
)

// NewBackend returns a Backend for the given type.
func NewBackend(bt BackendType) (Backend, error) {
	switch bt {
	case BackendRlimits:
		return &RlimitsBackend{}, nil
	case BackendDynamoRIO:
		return &DynamoRIOBackend{}, nil
	default:
		return nil, fmt.Errorf("unknown backend: %q", bt)
	}
}
