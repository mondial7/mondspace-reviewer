package main

import (
	"fmt"
	"net"
)

// checkBind refuses to serve a review to anything but this machine unless it
// was asked to (ADR 0030).
//
// msr serves your source, your diffs and your review notes over plain HTTP with
// no authentication of any kind. On loopback that is fine — it is your own
// screen. On a network it is a file server for your repository, and nothing
// about `--addr=0.0.0.0:7777` says so at the moment somebody types it.
//
// A malformed address is passed through: net.Listen gives a better message
// about it than this could.
func checkBind(addr string, allowRemote bool) error {
	if allowRemote {
		return nil
	}

	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil // let the listener explain
	}
	// An empty host means every interface, which is the same exposure as
	// 0.0.0.0 and is easy to write by accident as ":7777".
	if host == "" {
		return nil
	}
	if host == "localhost" {
		return nil
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return nil // a name msr cannot resolve; the listener will try
	}
	if ip.IsLoopback() {
		return nil
	}

	return fmt.Errorf(
		"%s is not this machine, and msr serves your source, your diffs and your "+
			"review notes with no authentication at all.\n"+
			"If you meant it — a container, or a machine you trust the network of — "+
			"pass --allow-remote.", addr)
}
