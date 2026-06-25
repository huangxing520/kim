package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRecover(t *testing.T) {
	assert.NotPanics(t, func() {
		defer Recover("test-location")
		panic("test panic")
	})
}

func TestRecoverNoPanic(t *testing.T) {
	assert.NotPanics(t, func() {
		defer Recover("test-no-panic")
	})
}

func TestGoSafe(t *testing.T) {
	done := make(chan struct{})
	GoSafe("test-goroutine", func() {
		defer close(done)
		panic("test panic in goroutine")
	})
	<-done
}

func TestSafeRecover(t *testing.T) {
	called := make(chan interface{}, 1)
	go func() {
		defer SafeRecover("test-safe", func(r interface{}) {
			called <- r
		})
		panic("expected panic")
	}()
	r := <-called
	assert.Equal(t, "expected panic", r)
}
