package client

import (
	"sync"
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

func TestRoundRobinEmpty(t *testing.T) {
	rr := newRoundRobin()
	ids := []string{}

	id := rr.Next(ids)
	if id != "" {
		t.Errorf("got %q, want empty string for empty ids", id)
	}
}

func TestRoundRobinOrder(t *testing.T) {
	rr := newRoundRobin()
	ids := []string{"a", "b", "c"}

	expected := []string{"a", "b", "c", "a", "b", "c"}
	for i, want := range expected {
		got := rr.Next(ids)
		if got != want {
			t.Errorf("call %d: got %s, want %s", i, got, want)
		}
	}
}

func TestRoundRobinConcurrent(t *testing.T) {
	rr := newRoundRobin()
	ids := []string{"a", "b", "c"}
	const goroutines = 10
	const callsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < callsPerGoroutine; j++ {
				id := rr.Next(ids)
				if id != "a" && id != "b" && id != "c" {
					t.Errorf("unexpected id: %s", id)
				}
			}
		}()
	}
	wg.Wait()
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

func TestPoolGetAnyExcludingEmpty(t *testing.T) {
	p := &Pool{
		conns: make(map[string]*grpc.ClientConn),
		rr:    newRoundRobin(),
	}
	_, err := p.GetAnyExcluding("any")
	if err == nil {
		t.Error("expected error for empty pool")
	}
}

func TestPoolGetAnyExcludingAll(t *testing.T) {
	p := &Pool{
		conns: map[string]*grpc.ClientConn{
			"only": {},
		},
		rr: newRoundRobin(),
	}
	_, err := p.GetAnyExcluding("only")
	if err == nil {
		t.Error("expected error when excluding all available instances")
	}
}
