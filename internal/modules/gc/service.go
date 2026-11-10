package gc

import (
	"context"

	"github.com/HamzaGbada/orca/internal/modules/inventory"
)

// Service plans garbage collection for the connected engine. It only reads.
type Service struct {
	collector *inventory.Collector
	policy    Policy
}

func NewService(c *inventory.Collector, p Policy) *Service {
	return &Service{collector: c, policy: p}
}

// Analyze collects a fresh inventory and plans it under the policy.
func (s *Service) Analyze(ctx context.Context) (*Analysis, error) {
	inv, err := s.collector.Collect(ctx)
	if err != nil {
		return nil, err
	}
	return Analyze(inv, s.policy)
}
