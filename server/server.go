// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/gotmc/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package server

import (
	"sync"

	"github.com/gotmc/hislip/protocol"
)

// Server serves one instrument over HiSLIP.
//
// The server owns sessions and the device; it does not own a listener. Accepting
// connections is transport-specific and belongs to a host or firmware adapter,
// which hands each accepted Stream to the server. That split is what lets the
// same server run over a host socket, a W5500 socket and an in-memory pipe.
type Server struct {
	cfg Config
	dev Device

	mu       sync.Mutex
	sessions map[uint16]*Session
	nextID   uint16
}

// New returns a server for the given device.
//
// The zero Config yields the PoE-to-GPIB profile: one session, Synchronized Mode
// only, HiSLIP 1.0. Defaults are applied before validation, so New(dev,
// Config{}) is the normal call.
func New(dev Device, cfg Config) (*Server, error) {
	if dev == nil {
		return nil, ErrInvalidConfig
	}
	cfg = cfg.withDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &Server{
		cfg:      cfg,
		dev:      dev,
		sessions: make(map[uint16]*Session),
		nextID:   1,
	}, nil
}

// Config returns the effective configuration, with defaults applied.
func (s *Server) Config() Config { return s.cfg }

// Device returns the instrument backend.
func (s *Server) Device() Device { return s.dev }

// Sessions returns the number of live sessions.
func (s *Server) Sessions() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sessions)
}

// Session returns the session with the given ID, or nil.
func (s *Server) Session(id uint16) *Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessions[id]
}

// allocateSession creates a session and registers it, or returns ErrMaxClients.
//
// Session IDs start at 1 rather than 0. Zero is a legal session ID on the wire,
// but reserving it makes an uninitialised or defaulted value distinguishable
// from a real session in logs and in a debugger, which is worth one value out of
// 65536.
func (s *Server) allocateSession(
	version protocol.Version,
	syncStream Stream,
) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.sessions) >= s.cfg.MaxSessions {
		return nil, ErrMaxClients
	}
	id, ok := s.allocateIDLocked()
	if !ok {
		return nil, ErrMaxClients
	}
	sess := newSession(id, version, syncStream, s.cfg)
	s.sessions[id] = sess
	return sess, nil
}

// allocateIDLocked returns an unused session ID. The caller holds s.mu.
func (s *Server) allocateIDLocked() (uint16, bool) {
	// Scan the whole 16-bit space at most once, so that a pathological pattern
	// of session churn cannot loop forever.
	for i := 0; i < 1<<16; i++ {
		id := s.nextID
		s.nextID++
		if s.nextID == 0 {
			s.nextID = 1
		}
		if _, inUse := s.sessions[id]; !inUse && id != 0 {
			return id, true
		}
	}
	return 0, false
}

// removeSession unregisters a session and closes it.
func (s *Server) removeSession(sess *Session) error {
	if sess == nil {
		return nil
	}
	s.mu.Lock()
	delete(s.sessions, sess.id)
	s.mu.Unlock()
	return sess.close()
}

// CloseAll ends every session. It is used on shutdown and by tests.
func (s *Server) CloseAll() error {
	s.mu.Lock()
	all := make([]*Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		all = append(all, sess)
	}
	s.sessions = make(map[uint16]*Session)
	s.mu.Unlock()

	var firstErr error
	for _, sess := range all {
		if err := sess.close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
