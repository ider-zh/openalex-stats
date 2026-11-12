package main

import (
	"net"
	"testing"
	"time"
)

func TestWarmHandoffLoadsBeforeWaitingForOccupiedListener(t *testing.T) {
	previous, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer previous.Close()
	address := previous.Addr().String()
	if listener, err := listenForHandoff(address, 0, func() { t.Error("zero wait announced readiness") }); err == nil {
		listener.Close()
		t.Fatal("occupied listener unexpectedly replaced")
	}
	ready := false
	current, err := listenForHandoff(address, 2*time.Second, func() { ready = true; previous.Close() })
	if err != nil {
		t.Fatal(err)
	}
	defer current.Close()
	if !ready || current.Addr().String() != address {
		t.Fatal("handoff did not wait on the occupied port")
	}
}
