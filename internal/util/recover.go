package util

import (
	"runtime/debug"

	"github.com/klintcheng/kim/internal/logger"
)

func Recover(location string) {
	if r := recover(); r != nil {
		logger.CommonLogger.Errorf("panic recovered in %s: %v\n%s", location, r, debug.Stack())
	}
}

func SafeRecover(location string, onRecover func(r interface{})) {
	if r := recover(); r != nil {
		logger.CommonLogger.Errorf("panic recovered in %s: %v\n%s", location, r, debug.Stack())
		if onRecover != nil {
			onRecover(r)
		}
	}
}

func GoSafe(location string, fn func()) {
	go func() {
		defer Recover(location)
		fn()
	}()
}
