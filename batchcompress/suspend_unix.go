//go:build !windows

package main

import (
	"log"
	"syscall"
)

func suspendPID(pid int) error {
	if err := syscall.Kill(pid, syscall.SIGSTOP); err != nil {
		return err
	}
	log.Printf("paused ffmpeg pid=%d (SIGSTOP)", pid)
	return nil
}

func resumePID(pid int) error {
	if err := syscall.Kill(pid, syscall.SIGCONT); err != nil {
		return err
	}
	log.Printf("resumed ffmpeg pid=%d (SIGCONT)", pid)
	return nil
}

// killTree terminates pid. Unix doesn't have a launcher-shim problem in
// practice, so we don't walk the tree here.
func killTree(pid int) {
	_ = syscall.Kill(pid, syscall.SIGKILL)
}
