//go:build !windows

package main

import "net"

func connectIPC(path string) (net.Conn, error) {
	return net.Dial("unix", path)
}
