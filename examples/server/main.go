// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/gotmc/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

// Command server runs a HiSLIP server in front of a simulated SCPI instrument.
//
// It exists to be driven by third-party clients. No HiSLIP-capable instrument is
// available on the development bench, so conformance is established against
// independent implementations instead: NI-VISA, Keysight IO Libraries, pyvisa-py
// and lxi-tools/libhislip. See §24.4 of the design specification.
//
// Usage:
//
//	go run ./examples/server -addr :4880
//	python -c "import pyvisa; rm=pyvisa.ResourceManager('@py'); \
//	  print(rm.open_resource('TCPIP0::127.0.0.1::hislip0::INSTR').query('*IDN?'))"
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/gotmc/hislip"
	"github.com/gotmc/hislip/server"
)

func main() {
	addr := flag.String("addr", ":4880", "address to listen on")
	verbose := flag.Bool("v", false, "log every instrument operation")
	flag.Parse()

	inst := &instrument{verbose: *verbose}
	srv, err := server.New(inst, server.Config{})
	if err != nil {
		log.Fatalf("creating server: %v", err)
	}

	l, err := hislip.Listen(*addr, srv)
	if err != nil {
		log.Fatalf("listening on %s: %v", *addr, err)
	}
	l.OnError(func(err error) {
		// A client closing mid-session is normal, not a fault.
		if errors.Is(err, context.Canceled) {
			return
		}
		log.Printf("connection: %v", err)
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Printf("hislip server listening on %s", l.Addr())
	log.Printf("resource: TCPIP0::127.0.0.1::hislip0::INSTR")

	if err := l.Serve(ctx); err != nil {
		log.Fatalf("serve: %v", err)
	}

	c := srv.Counters()
	log.Printf(
		"sessions=%d refused=%d fatal=%d nonfatal=%d silent-interrupted=%d "+
			"interrupted-sent=%d no-answer=%d clears=%d triggers=%d polls=%d",
		c.SessionsOpened, c.SessionsRefused, c.FatalErrors, c.NonFatalErrors,
		c.SilentInterrupted, c.InterruptedSent, c.QueriesWithoutAnswer,
		c.DeviceClears, c.Triggers, c.SerialPolls,
	)
}

// instrument is a simulated SCPI instrument.
//
// It implements just enough IEEE 488.2 to satisfy a VISA client: *IDN?, *RST,
// *CLS, *OPC?, *ESR?, *STB?, plus a couple of measurement queries. Anything else
// sets the command-error bit, which is what a real instrument does and what makes
// a client's error handling exercisable.
type instrument struct {
	verbose bool

	mu        sync.Mutex
	input     []byte
	response  []byte
	status    byte
	esr       byte
	voltage   float64
	triggered int
}

const (
	statusESB = 1 << 5 // event status bit
	esrCME    = 1 << 5 // command error
)

func (d *instrument) logf(format string, args ...any) {
	if d.verbose {
		log.Printf("instrument: "+format, args...)
	}
}

// Write implements server.Device.
func (d *instrument) Write(_ context.Context, p []byte, end bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.input = append(d.input, p...)
	if !end {
		return nil
	}
	program := strings.TrimRight(string(d.input), "\r\n")
	d.input = d.input[:0]
	d.logf("write %q", program)
	d.execute(program)
	return nil
}

// execute runs a program message, appending any response.
func (d *instrument) execute(program string) {
	for _, cmd := range strings.Split(program, ";") {
		cmd = strings.TrimSpace(cmd)
		if cmd == "" {
			continue
		}
		d.executeOne(cmd)
	}
}

func (d *instrument) executeOne(cmd string) {
	upper := strings.ToUpper(cmd)
	switch {
	case upper == "*IDN?":
		d.reply("GoTMC,HiSLIP Simulated Instrument,0,0.1.0")
	case upper == "*RST":
		d.voltage = 0
		d.triggered = 0
		d.esr = 0
		d.status = 0
	case upper == "*CLS":
		d.esr = 0
		d.status &^= statusESB
	case upper == "*OPC?":
		d.reply("1")
	case upper == "*ESR?":
		d.reply(strconv.Itoa(int(d.esr)))
		d.esr = 0
		d.status &^= statusESB
	case upper == "*STB?":
		d.reply(strconv.Itoa(int(d.status)))
	case upper == "*TST?":
		d.reply("0")
	case upper == "SYST:ERR?" || upper == "SYSTEM:ERROR?":
		if d.esr&esrCME != 0 {
			d.reply("-113,\"Undefined header\"")
			d.esr &^= esrCME
		} else {
			d.reply("0,\"No error\"")
		}
	case upper == "MEAS:VOLT?" || upper == "MEASURE:VOLTAGE?":
		d.reply(fmt.Sprintf("%.6E", d.voltage))
	case upper == "TRIG:COUNT?":
		d.reply(strconv.Itoa(d.triggered))
	case strings.HasPrefix(upper, "SOUR:VOLT ") || strings.HasPrefix(upper, "SOURCE:VOLTAGE "):
		fields := strings.Fields(cmd)
		if len(fields) == 2 {
			if v, err := strconv.ParseFloat(fields[1], 64); err == nil {
				d.voltage = v
				return
			}
		}
		d.commandError()
	case upper == "CURV?" || upper == "WAV:DATA?":
		// A binary block, to exercise chunked responses and the query detector's
		// handling of block data on the way back in.
		payload := make([]byte, 4096)
		for i := range payload {
			payload[i] = byte(i)
		}
		header := fmt.Sprintf("#4%04d", len(payload))
		d.response = append(d.response, header...)
		d.response = append(d.response, payload...)
		d.response = append(d.response, '\n')
	default:
		d.commandError()
	}
}

func (d *instrument) reply(s string) {
	d.response = append(d.response, s...)
	d.response = append(d.response, '\n')
}

func (d *instrument) commandError() {
	d.esr |= esrCME
	d.status |= statusESB
	d.logf("command error")
}

// Read implements server.Device. A response longer than p is delivered across
// several calls, with end set only on the last.
func (d *instrument) Read(_ context.Context, p []byte) (int, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.response) == 0 {
		// No response queued. This is the condition §17.3 defines behaviour for,
		// and it is what a client sees when it queries something the instrument
		// did not answer.
		return 0, false, errors.New("instrument: no response available")
	}
	n := copy(p, d.response)
	d.response = d.response[n:]
	end := len(d.response) == 0
	d.logf("read %d bytes, end=%v", n, end)
	return n, end, nil
}

// Clear implements server.Device.
func (d *instrument) Clear(context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.input = d.input[:0]
	d.response = d.response[:0]
	d.logf("clear")
	return nil
}

// Trigger implements server.Device.
func (d *instrument) Trigger(context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.triggered++
	d.logf("trigger (%d)", d.triggered)
	return nil
}

// ReadStatusByte implements server.Device. The server adjusts MAV; every other
// bit is the instrument's.
func (d *instrument) ReadStatusByte(context.Context) (byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.logf("status byte %#02x", d.status)
	return d.status, nil
}

// RemoteLocal implements server.Device.
func (d *instrument) RemoteLocal(_ context.Context, mode server.RemoteLocalMode) error {
	d.logf("remote/local: %v", mode)
	return nil
}

// DeviceName implements server.DeviceInfo.
func (d *instrument) DeviceName() string { return "GoTMC HiSLIP Simulated Instrument" }

var (
	_ server.Device     = (*instrument)(nil)
	_ server.DeviceInfo = (*instrument)(nil)
)
