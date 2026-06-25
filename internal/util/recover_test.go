package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRecover(t *testing.T) {
	assert.NotPanics(t, func() {
		defer Recover("test-location")
		panic("test panic message")
	})
}

func TestRecoverNoPanic(t *testing.T) {
	assert.NotPanics(t, func() {
		defer Recover("test-no-panic")
		_ = 1 + 1
	})
}

func TestGoSafe(t *testing.T) {
	done := make(chan struct{})
	GoSafe("test-goroutine", func() {
		defer close(done)
		panic("test panic in GoSafe")
	})
	<-done
}

func TestSafeRecover(t *testing.T) {
	called := make(chan interface{}, 1)
	go func() {
		defer SafeRecover("test-goroutine", func(r interface{}) {
			called <- r
		})
		panic("test panic for SafeRecover")
	}()
	r := <-called
	assert.Equal(t, "test panic for SafeRecover", r)
}
