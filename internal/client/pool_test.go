package client

import (
	"testing"

	"google.golang.org/grpc"
)

func TestRoundRobin(t *testing.T) {
	rr := newRoundRobin()
	ids := []string{"a", "b", "c"}

	results := make(map[string]int)
	for i := 0; i < 9; i++ {
		id := rr.Next(ids)
		results[id]++
	}

	for _, id := range ids {
		if results[id] != 3 {
			t.Errorf("id %s got %d, want 3", id, results[id])
		}
	}
}

func TestRoundRobinSingle(t *testing.T) {
	rr := newRoundRobin()
	ids := []string{"only"}

	for i := 0; i < 5; i++ {
		id := rr.Next(ids)
		if id != "only" {
			t.Errorf("got %s, want only", id)
		}
	}
}

func TestPoolGetEmpty(t *testing.T) {
	p := &Pool{
		conns: make(map[string]*grpc.ClientConn),
		rr:    newRoundRobin(),
	}
	_, err := p.Get("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent service")
	}

	_, err = p.GetAny()
	if err == nil {
		t.Error("expected error for empty pool")
	}
}
