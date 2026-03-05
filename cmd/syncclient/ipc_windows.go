//go:build windows

package main

import (
	"net"
	"os"
	"time"
)

// pipeConn wraps an *os.File to satisfy net.Conn for Windows named pipes.
type pipeConn struct {
	f *os.File
}

func (p *pipeConn) Read(b []byte) (int, error)         { return p.f.Read(b) }
func (p *pipeConn) Write(b []byte) (int, error)        { return p.f.Write(b) }
func (p *pipeConn) Close() error                       { return p.f.Close() }
func (p *pipeConn) LocalAddr() net.Addr                { return pipeAddr(p.f.Name()) }
func (p *pipeConn) RemoteAddr() net.Addr               { return pipeAddr(p.f.Name()) }
func (p *pipeConn) SetDeadline(t time.Time) error      { return nil }
func (p *pipeConn) SetReadDeadline(t time.Time) error  { return nil }
func (p *pipeConn) SetWriteDeadline(t time.Time) error { return nil }

type pipeAddr string

func (a pipeAddr) Network() string { return "pipe" }
func (a pipeAddr) String() string  { return string(a) }

func connectIPC(path string) (net.Conn, error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	return &pipeConn{f: f}, nil
}
