package kim

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestShutdownClosesChannels(t *testing.T) {
	s := NewServer(":0", &fakeServiceReg{}, &fakeUpgrader{})
	s.SetChannelMap(NewChannels())

	var closed int32
	fake := &fakeChannel{id: "test-ch"}
	fake.closeFn = func() error {
		atomic.StoreInt32(&closed, 1)
		return nil
	}
	s.ChannelMap.Add(fake)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown returned error: %v", err)
	}

	if atomic.LoadInt32(&closed) == 0 {
		t.Error("Shutdown did not close registered channel (CAS logic inverted)")
	}
}

func TestShutdownIdempotent(t *testing.T) {
	s := NewServer(":0", &fakeServiceReg{}, &fakeUpgrader{})
	s.SetChannelMap(NewChannels())
	ctx := context.Background()

	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("first Shutdown: %v", err)
	}
	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("second Shutdown: %v", err)
	}
}

type fakeChannel struct {
	Channel
	id      string
	closeFn func() error
}

func (f *fakeChannel) ID() string    { return f.id }
func (f *fakeChannel) Close() error  { if f.closeFn != nil { return f.closeFn() }; return nil }
func (f *fakeChannel) GetMeta() Meta { return nil }
func (f *fakeChannel) Push([]byte) error { return nil }

type fakeUpgrader struct {
	Upgrader
}

func (f *fakeUpgrader) Name() string { return "test-upgrader" }

type fakeServiceReg struct {
	ServiceRegistration
}

func (f *fakeServiceReg) ServiceID() string   { return "test-service-id" }
func (f *fakeServiceReg) ServiceName() string { return "test-service-name" }
func (f *fakeServiceReg) GetMeta() map[string]string { return nil }
