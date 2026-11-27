//go:build !unix

package main

import "syscall"

func asOwner(string) *syscall.SysProcAttr { return nil }

func guardSignals() func() { return func() {} }
