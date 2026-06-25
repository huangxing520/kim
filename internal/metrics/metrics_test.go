package metrics

import (
	"testing"
)

func TestNoDuplicateRegistration(t *testing.T) {
	if GRPCServerHandledTotal == nil {
		t.Fatal("GRPCServerHandledTotal is nil")
	}
	if GRPCCircuitBreakerState == nil {
		t.Fatal("GRPCCircuitBreakerState is nil")
	}
	if GRPCRetryTotal == nil {
		t.Fatal("GRPCRetryTotal is nil")
	}
	if GRPCRateLimitRejected == nil {
		t.Fatal("GRPCRateLimitRejected is nil")
	}
	if GRPCServerHandlingSeconds == nil {
		t.Fatal("GRPCServerHandlingSeconds is nil")
	}
}
