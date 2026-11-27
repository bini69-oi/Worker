//go:build unix

package main

import (
	"syscall"
	"testing"
	"time"
)

// Пока защита включена, SIGHUP (обрыв ssh) и SIGTERM не убивают процесс. Если защита не работает, умрёт сам тест.
func TestGuardSignalsSurvivesHangup(t *testing.T) {
	release := guardSignals()
	for _, sig := range []syscall.Signal{syscall.SIGHUP, syscall.SIGTERM, syscall.SIGINT} {
		if err := syscall.Kill(syscall.Getpid(), sig); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(100 * time.Millisecond)
	release()
}
