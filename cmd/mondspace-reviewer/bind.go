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
	// An empty host is every interface: `:7777` and `0.0.0.0:7777` reach the
	// same listener, and refusing one spelling while allowing the other is a
	// guard that can be got around by typing less (ADR 0055). The default is
	// 127.0.0.1, so nobody arrives here without having chosen an address.
	if host == "" {
		return remoteRefused(addr)
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

	return remoteRefused(addr)
}

// remoteRefused is what msr says when it is asked to serve a review to
// something that is not this machine.
func remoteRefused(addr string) error {
	return fmt.Errorf(
		"%s is not this machine, and msr serves your source, your diffs and your "+
			"review notes with no authentication at all.\n"+
			"If you meant it — a phone on your own network, a container, a machine "+
			"you trust the network of — pass --allow-remote.", addr)
}

// LANAddresses is where else this listener can be reached, for a reviewer who
// asked for that and now has to type it into a phone.
//
// Link-local and loopback are left out: one is not routable from another
// device and the other is the address they already have.
func LANAddresses(port string) []string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}

	var out []string
	for _, a := range addrs {
		net, ok := a.(*net.IPNet)
		if !ok || net.IP.IsLoopback() || net.IP.IsLinkLocalUnicast() {
			continue
		}
		ip := net.IP.To4()
		if ip == nil {
			continue // one address per interface is enough to type
		}
		out = append(out, "http://"+ip.String()+":"+port)
	}
	return out
}
