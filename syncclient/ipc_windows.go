//go:build windows

package main

import (
	"net"

	"github.com/Microsoft/go-winio"
)

func connectIPC(path string) (net.Conn, error) {
	return winio.DialPipe(path, nil)
}
