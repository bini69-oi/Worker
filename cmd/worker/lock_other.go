//go:build !unix

package main

// На системах без flock замка нет: Worker рассчитан на сервер с Linux.
func lockDB(string) (func(), error) { return func() {}, nil }

func privateFiles() {}
