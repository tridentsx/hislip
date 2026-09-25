// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/tridentsx/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package hislip

import (
	"context"
	"errors"
	"net"
	"strconv"
	"sync"

	"github.com/tridentsx/hislip/protocol"
	"github.com/tridentsx/hislip/server"
)

// Listener accepts HiSLIP connections and hands them to a server.
//
// This lives in the root package rather than in server/ because it uses net, and
// the server core must stay free of any transport so that it compiles under
// TinyGo. The split is the whole reason server.Stream exists: net.Conn satisfies
// it here, and a W5500 socket satisfies it in firmware.
//
// HiSLIP needs two connections to the same port, so nothing here distinguishes
// them. Each accepted connection is handed to server.Serve, which reads its first
// message to find out which channel it is.
type Listener struct {
	srv *server.Server
	ln  net.Listener

	mu      sync.Mutex
	closed  bool
	conns   map[net.Conn]struct{}
	wg      sync.WaitGroup
	onError func(error)
}

// Listen returns a Listener bound to addr, which may be an empty host to listen
// on all interfaces. An empty port defaults to the conventional HiSLIP port.
func Listen(addr string, srv *server.Server) (*Listener, error) {
	if srv == nil {
		return nil, errors.New("hislip: nil server")
	}
	if addr == "" {
		addr = ":" + strconv.Itoa(protocol.DefaultPort)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	return &Listener{
		srv:   srv,
		ln:    ln,
		conns: make(map[net.Conn]struct{}),
	}, nil
}

// Addr returns the address the listener is bound to, which is how a caller
// discovers the port when it asked for an arbitrary one.
func (l *Listener) Addr() net.Addr { return l.ln.Addr() }

// OnError installs a callback for connection-level errors. Without one they are
// discarded, because a connection error is normal operation: a client closing
// mid-session is not a server fault.
func (l *Listener) OnError(f func(error)) {
	l.mu.Lock()
	l.onError = f
	l.mu.Unlock()
}

// Serve accepts connections until the listener is closed or ctx is cancelled.
//
// Each connection runs in its own goroutine, which is where the host adapter and
// a firmware adapter diverge: firmware would run these as tasks under whichever
// scheduler the build selected. Neither decision belongs in the server core.
func (l *Listener) Serve(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		_ = l.Close()
	}()

	for {
		conn, err := l.ln.Accept()
		if err != nil {
			l.mu.Lock()
			closed := l.closed
			l.mu.Unlock()
			if closed || ctx.Err() != nil {
				l.wg.Wait()
				return nil
			}
			return err
		}

		l.track(conn)
		l.wg.Go(func() {
			defer l.untrack(conn)
			if err := l.srv.Serve(ctx, conn); err != nil {
				l.report(err)
			}
		})
	}
}

// ListenAndServe binds addr and serves until ctx is cancelled. It is the
// one-liner for a host server.
func ListenAndServe(ctx context.Context, addr string, srv *server.Server) error {
	l, err := Listen(addr, srv)
	if err != nil {
		return err
	}
	return l.Serve(ctx)
}

// Close stops accepting and closes every live connection.
//
// Closing the connections rather than only the listener matters: a HiSLIP session
// holds two long-lived connections that carry no periodic traffic, so a session
// left open would keep the process alive indefinitely.
func (l *Listener) Close() error {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil
	}
	l.closed = true
	conns := make([]net.Conn, 0, len(l.conns))
	for c := range l.conns {
		conns = append(conns, c)
	}
	l.mu.Unlock()

	err := l.ln.Close()
	for _, c := range conns {
		_ = c.Close()
	}
	_ = l.srv.CloseAll()
	return err
}

func (l *Listener) track(conn net.Conn) {
	l.mu.Lock()
	l.conns[conn] = struct{}{}
	l.mu.Unlock()
}

func (l *Listener) untrack(conn net.Conn) {
	l.mu.Lock()
	delete(l.conns, conn)
	l.mu.Unlock()
}

func (l *Listener) report(err error) {
	l.mu.Lock()
	f := l.onError
	l.mu.Unlock()
	if f != nil {
		f(err)
	}
}

// compile-time proof that a net.Conn is usable as a server.Stream, which is what
// makes this adapter as small as it is.
var _ server.Stream = (net.Conn)(nil)
