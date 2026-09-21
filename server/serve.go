// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/gotmc/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package server

import (
	"context"
	"errors"

	"github.com/gotmc/hislip/protocol"
)

// Counters are the per-server diagnostic counters of §52.
//
// A requirement to count an event is not satisfied by a log line, because logs
// are not retained on a device whose diagnostic serial port is normally
// disconnected. These are read through Server.Counters.
type Counters struct {
	SessionsOpened       uint64
	SessionsRefused      uint64
	FatalErrors          uint64
	NonFatalErrors       uint64
	WrongChannel         uint64
	SilentInterrupted    uint64
	InterruptedSent      uint64
	QueriesWithoutAnswer uint64
	TruncatedResponses   uint64
	DeviceClears         uint64
	Triggers             uint64
	SerialPolls          uint64
	ServiceRequests      uint64
	OversizedMessages    uint64
}

// Serve runs a newly accepted connection to completion.
//
// A HiSLIP connection does not declare which of the two channels it is; the
// server learns that from the first message, Initialize for the synchronous
// channel and AsyncInitialize for the asynchronous one. That matches the W5500
// socket model, where a listening socket becomes the connection and the first
// bytes are the only clue.
//
// Serve blocks until the connection ends. It does not spawn the accept loop or
// decide how concurrency is arranged: a host adapter runs it in a goroutine, a
// firmware adapter in a task. Keeping that decision outside the core is what lets
// the same server run over a host socket, a W5500 socket and an in-memory pipe.
func (s *Server) Serve(ctx context.Context, stream Stream) error {
	var hdrBuf [protocol.HeaderSize]byte
	h, err := protocol.ReadHeader(stream, &hdrBuf)
	if err != nil {
		_ = stream.Close()
		return err
	}

	switch h.Type {
	case protocol.Initialize:
		return s.serveSyncChannel(ctx, stream, h)
	case protocol.AsyncInitialize:
		return s.serveAsyncChannel(ctx, stream, h)
	default:
		// Neither initialization message. The connection cannot become a
		// channel, so it is refused with fatal error 3 and closed. Telling the
		// peer why is worth the two writes: silence here presents as a hang.
		w := NewMessageWriter(stream, protocol.ChannelSynchronous)
		_ = w.WriteFatalError(
			protocol.FatalInvalidInitSequence,
			"expected Initialize or AsyncInitialize",
		)
		_ = stream.Close()
		s.count(func(c *Counters) { c.FatalErrors++ })
		return ErrInvalidInitSequence
	}
}

// serveSyncChannel completes the Initialize transaction and runs the synchronous
// channel.
func (s *Server) serveSyncChannel(
	ctx context.Context,
	stream Stream,
	h protocol.Header,
) error {
	w := NewMessageWriter(stream, protocol.ChannelSynchronous)

	// The sub-address is the payload. It is read through a bounded buffer, not one
	// sized from the wire.
	sub, err := readSubAddress(stream, h, s.cfg.MaxRxPayload)
	if err != nil {
		_ = w.WriteFatalError(protocol.FatalPoorlyFormedHeader, "bad sub-address")
		_ = stream.Close()
		return err
	}

	sess, resp, err := s.Initialize(h, sub, stream)
	if err != nil {
		wire := WireErrorFor(err)
		_ = w.WriteFatalError(wire.FatalCode, err.Error())
		_ = stream.Close()
		if errors.Is(err, ErrMaxClients) {
			s.count(func(c *Counters) { c.SessionsRefused++ })
		}
		s.count(func(c *Counters) { c.FatalErrors++ })
		return err
	}
	if err := w.Write(resp, nil); err != nil {
		_ = s.removeSession(sess)
		return err
	}
	s.count(func(c *Counters) { c.SessionsOpened++ })

	sess.setSyncWriter(w)
	defer func() { _ = s.removeSession(sess) }()

	// Wait for the asynchronous channel before serving operations. Until both
	// exist the session must refuse normal work with fatal error 2, which
	// Session.requireReady enforces; the loop below simply will not see traffic
	// it can act on until pairing completes.
	return s.runSync(ctx, sess, stream)
}

// serveAsyncChannel completes the AsyncInitialize transaction and runs the
// asynchronous channel.
func (s *Server) serveAsyncChannel(
	ctx context.Context,
	stream Stream,
	h protocol.Header,
) error {
	w := NewMessageWriter(stream, protocol.ChannelAsynchronous)

	sess, resp, err := s.AsyncInitialize(h, stream)
	if err != nil {
		// A fatal error is reported on the synchronous channel by convention, but
		// there is no session to find one for, so it goes out here.
		fw := NewMessageWriter(stream, protocol.ChannelSynchronous)
		wire := WireErrorFor(err)
		_ = fw.WriteFatalError(wire.FatalCode, err.Error())
		_ = stream.Close()
		s.count(func(c *Counters) { c.FatalErrors++ })
		return err
	}
	if err := w.Write(resp, nil); err != nil {
		return err
	}
	sess.setAsyncWriter(w)

	return s.runAsync(ctx, sess, stream)
}

// runSync is the synchronous channel loop.
func (s *Server) runSync(ctx context.Context, sess *Session, stream Stream) error {
	r := newReader(stream, protocol.ChannelSynchronous, sess.MaxRxPayload())
	go r.run()
	defer r.stop()

	sess.setSyncPending(r.pending)

	tx := sess.Transaction()

	// Allocated once at the server's configured maximum, then sliced per response
	// to whatever the session's transmit limit currently is. The limit can be
	// lowered at any time by the maximum message size transaction, so it cannot
	// be captured here.
	buf := make([]byte, s.cfg.MaxTxPayload)

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		msg, ok := r.next()
		if !ok {
			return nil
		}
		err := s.handleSync(ctx, sess, tx, msg, buf)
		r.release(msg.buf)
		if err != nil {
			return err
		}
	}
}

// handleSync processes one synchronous-channel message.
func (s *Server) handleSync(
	ctx context.Context,
	sess *Session,
	tx *Transaction,
	msg message,
	buf []byte,
) error {
	w := sess.SyncWriter()

	if msg.err != nil {
		return s.reportReadError(sess, w, msg)
	}

	// A message arriving during a Device Clear is accepted and discarded until
	// DeviceClearComplete is found. The payload has already been drained by the
	// reader, so discarding here keeps the stream synchronised.
	if DiscardDuringClear(sess, msg.header) {
		return nil
	}

	class, err := s.Accept(sess, msg.header, protocol.ChannelSynchronous)
	if err != nil {
		return s.reportAcceptError(sess, w, err)
	}

	switch msg.header.Type {
	case protocol.Data, protocol.DataEnd:
		return s.handleData(ctx, sess, tx, msg, w, buf)

	case protocol.Trigger:
		interrupted, err := s.Trigger(ctx, sess, tx, msg.header)
		s.countInterrupted(interrupted)
		if err != nil {
			return s.reportOperationError(sess, w, err)
		}
		s.count(func(c *Counters) { c.Triggers++ })
		return nil

	case protocol.DeviceClearComplete:
		ack, clearErr := s.CompleteDeviceClear(ctx, sess, tx, msg.header)
		if werr := w.Write(ack, nil); werr != nil {
			return werr
		}
		s.count(func(c *Counters) { c.DeviceClears++ })
		if clearErr != nil {
			return s.reportOperationError(sess, w, clearErr)
		}
		return nil

	case protocol.FatalError:
		// The client has reported a fatal condition and will close.
		s.count(func(c *Counters) { c.FatalErrors++ })
		return errClientFatal

	case protocol.Error:
		s.count(func(c *Counters) { c.NonFatalErrors++ })
		return nil

	case protocol.Initialize:
		// A second Initialize on an established channel.
		_ = w.WriteFatalError(protocol.FatalInvalidInitSequence, "already initialized")
		return ErrInvalidInitSequence
	}

	if class == ClassSyncOrdered && msg.header.Type.VendorSpecific() {
		// The vendor extension is not enabled, so an unrecognised vendor message
		// is refused with the code the standard reserves for it.
		s.count(func(c *Counters) { c.NonFatalErrors++ })
		return w.WriteError(
			protocol.NonFatalUnrecognizedVendorMsg,
			"vendor extension not enabled",
		)
	}
	s.count(func(c *Counters) { c.NonFatalErrors++ })
	return w.WriteError(protocol.NonFatalUnrecognizedMessageType, "unsupported message")
}

// handleData delivers a program message chunk and, when the message is complete
// and a response is expected, streams the response back.
func (s *Server) handleData(
	ctx context.Context,
	sess *Session,
	tx *Transaction,
	msg message,
	w *MessageWriter,
	buf []byte,
) error {
	interrupted, err := s.ReceiveData(ctx, sess, tx, msg.header, msg.payload)
	s.countInterrupted(interrupted)
	if err != nil {
		return s.reportOperationError(sess, w, err)
	}

	sess.feedProgramMessage(msg.payload)
	if msg.header.Type != protocol.DataEnd {
		return nil
	}

	if sess.programDisposition() != ResponseExpected {
		return tx.Complete()
	}

	opCtx, release := sess.Operation(ctx)
	defer release()

	outcome, err := s.SendResponse(opCtx, sess, tx, Response{
		Sync:        w,
		Async:       sess.AsyncWriter(),
		MessageID:   msg.header.MessageID(),
		Scratch:     responseScratch(buf, sess.MaxTxPayload()),
		InputQueued: sess.syncPending(),
	})
	s.countOutcome(outcome)
	if err != nil {
		return s.reportOperationError(sess, w, err)
	}
	return nil
}

// runAsync is the asynchronous channel loop.
func (s *Server) runAsync(ctx context.Context, sess *Session, stream Stream) error {
	r := newReader(stream, protocol.ChannelAsynchronous, sess.MaxRxPayload())
	go r.run()
	defer r.stop()

	tx := sess.Transaction()

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		msg, ok := r.next()
		if !ok {
			return nil
		}
		err := s.handleAsync(ctx, sess, tx, msg)
		r.release(msg.buf)
		if err != nil {
			return err
		}
	}
}

// handleAsync processes one asynchronous-channel message.
//
// Every operation here either needs no bus access or may preempt one, which is
// what lets the asynchronous channel stay responsive while the synchronous channel
// is blocked on a slow instrument. That responsiveness is the whole reason HiSLIP
// has two connections. See §47.
func (s *Server) handleAsync(
	ctx context.Context,
	sess *Session,
	tx *Transaction,
	msg message,
) error {
	w := sess.AsyncWriter()
	if w == nil {
		return ErrSessionNotReady
	}
	if msg.err != nil {
		return s.reportReadError(sess, w, msg)
	}
	if _, err := s.Accept(sess, msg.header, protocol.ChannelAsynchronous); err != nil {
		return s.reportAcceptError(sess, w, err)
	}

	switch msg.header.Type {
	case protocol.AsyncDeviceClear:
		ack, err := s.BeginDeviceClear(sess, tx)
		if err != nil {
			return s.reportOperationError(sess, w, err)
		}
		return w.Write(ack, nil)

	case protocol.AsyncStatusQuery:
		resp, err := s.StatusQuery(ctx, sess, msg.header)
		s.count(func(c *Counters) { c.SerialPolls++ })
		if err != nil {
			return s.reportOperationError(sess, w, err)
		}
		return w.Write(resp, nil)

	case protocol.AsyncLock:
		resp, err := s.Lock(ctx, sess, msg.header, string(msg.payload))
		if err != nil {
			return s.reportOperationError(sess, w, err)
		}
		return w.Write(resp, nil)

	case protocol.AsyncLockInfo:
		resp, err := s.LockInfo(sess, msg.header)
		if err != nil {
			return s.reportOperationError(sess, w, err)
		}
		return w.Write(resp, nil)

	case protocol.AsyncRemoteLocalControl:
		resp, err := s.RemoteLocal(ctx, sess, msg.header)
		if err != nil {
			return s.reportOperationError(sess, w, err)
		}
		return w.Write(resp, nil)

	case protocol.AsyncMaximumMessageSize:
		resp, agreed := s.MaximumMessageSize(sess, decodeMaxSize(msg.payload))
		var payload [8]byte
		putUint64(payload[:], agreed)
		return w.Write(resp, payload[:])

	case protocol.AsyncInitialize:
		_ = w.Write(protocol.Header{
			Type:    protocol.FatalError,
			Control: uint8(protocol.FatalInvalidInitSequence),
		}, nil)
		return ErrInvalidInitSequence

	case protocol.FatalError:
		s.count(func(c *Counters) { c.FatalErrors++ })
		return errClientFatal

	case protocol.Error:
		s.count(func(c *Counters) { c.NonFatalErrors++ })
		return nil
	}

	s.count(func(c *Counters) { c.NonFatalErrors++ })
	return w.WriteError(protocol.NonFatalUnrecognizedMessageType, "unsupported message")
}

// errClientFatal reports that the peer sent a FatalError, so the session ends
// without the server sending one of its own.
var errClientFatal = errors.New("hislip: peer reported a fatal error")

// reportReadError answers a message the reader could not accept.
func (s *Server) reportReadError(sess *Session, w *MessageWriter, msg message) error {
	if errors.Is(msg.err, protocol.ErrMessageTooLarge) {
		s.count(func(c *Counters) { c.OversizedMessages++; c.NonFatalErrors++ })
		if w != nil {
			return w.WriteError(protocol.NonFatalMessageTooLarge, "message too large")
		}
		return nil
	}
	// Anything else means synchronisation is lost or the peer went away.
	return msg.err
}

// reportAcceptError answers a message the session refused.
func (s *Server) reportAcceptError(sess *Session, w *MessageWriter, err error) error {
	wire := WireErrorFor(err)
	if errors.Is(err, protocol.ErrWrongChannel) {
		s.count(func(c *Counters) { c.WrongChannel++ })
	}
	if wire.Fatal {
		s.count(func(c *Counters) { c.FatalErrors++ })
		if w != nil {
			_ = w.WriteFatalError(wire.FatalCode, err.Error())
		}
		return err
	}
	s.count(func(c *Counters) { c.NonFatalErrors++ })
	if w == nil {
		return nil
	}
	return w.WriteError(wire.Code, err.Error())
}

// reportOperationError answers a backend or operation failure.
//
// A backend failure is an operation error and must leave the session usable; a
// slow or absent instrument is not a protocol fault. Only a genuinely fatal
// condition ends the session. See R-SRV-032.
func (s *Server) reportOperationError(sess *Session, w *MessageWriter, err error) error {
	wire := WireErrorFor(err)
	if wire.Fatal {
		s.count(func(c *Counters) { c.FatalErrors++ })
		if w != nil {
			_ = w.WriteFatalError(wire.FatalCode, err.Error())
		}
		return err
	}
	s.count(func(c *Counters) { c.NonFatalErrors++ })
	if w == nil {
		return nil
	}
	// A backend failure has no IVI-defined code, so it is reported with a
	// device-defined one. A condition the codec recognised keeps the code
	// WireErrorFor computed for it; discarding that in favour of a fixed code,
	// as an earlier version did, told the client the wrong thing.
	code := wire.Code
	if code == protocol.NonFatalUnidentified {
		code = CodeHandshakeTimeout
	}
	return w.WriteError(code, err.Error())
}

// responseScratch returns buf sliced to the session's current transmit limit.
//
// The limit is honoured rather than merely checked, because a client is entitled
// to lower it mid-session and every response after that must fit.
func responseScratch(buf []byte, limit uint64) []byte {
	if limit == 0 || limit >= uint64(len(buf)) {
		return buf
	}
	return buf[:limit]
}

// readSubAddress reads the payload of an Initialize message as an ASCII
// sub-address, through a bounded buffer.
func readSubAddress(stream Stream, h protocol.Header, limit uint64) (string, error) {
	if err := protocol.CheckLength(h.Length, limit); err != nil {
		return "", err
	}
	if h.Length == 0 {
		return "", nil
	}
	buf := make([]byte, h.Length)
	if err := protocol.ReadPayload(stream, h.Length, buf, nil); err != nil {
		return "", err
	}
	return string(buf), nil
}

// decodeMaxSize reads the 8-byte big-endian maximum message size of an
// AsyncMaximumMessageSize payload. A short payload yields zero, which the
// handler treats as "use the server's maximum".
func decodeMaxSize(payload []byte) uint64 {
	if len(payload) < 8 {
		return 0
	}
	var v uint64
	for i := 0; i < 8; i++ {
		v = v<<8 | uint64(payload[i])
	}
	return v
}

// putUint64 writes v as 8 big-endian bytes.
func putUint64(b []byte, v uint64) {
	for i := 7; i >= 0; i-- {
		b[i] = byte(v)
		v >>= 8
	}
}

// count applies f to the server's counters.
func (s *Server) count(f func(*Counters)) {
	s.countersMu.Lock()
	f(&s.counters)
	s.countersMu.Unlock()
}

// Counters returns a snapshot of the diagnostic counters.
func (s *Server) Counters() Counters {
	s.countersMu.Lock()
	defer s.countersMu.Unlock()
	return s.counters
}

func (s *Server) countInterrupted(i Interrupted) {
	switch i {
	case InterruptedRMTMismatch:
		s.count(func(c *Counters) { c.SilentInterrupted++ })
	case InterruptedInputQueued:
		s.count(func(c *Counters) { c.InterruptedSent++ })
	}
}

func (s *Server) countOutcome(o ResponseOutcome) {
	switch o {
	case ResponseAbsent:
		s.count(func(c *Counters) { c.QueriesWithoutAnswer++ })
	case ResponseTruncated:
		s.count(func(c *Counters) { c.TruncatedResponses++ })
	case ResponseInterrupted:
		s.count(func(c *Counters) { c.InterruptedSent++ })
	}
}
