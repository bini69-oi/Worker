//go:build !unix

package main

import "syscall"

func asOwner(string) (*syscall.SysProcAttr, error) { return nil, nil }

func guardSignals() func() { return func() {} }
