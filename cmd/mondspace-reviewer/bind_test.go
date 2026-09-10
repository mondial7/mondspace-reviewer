package main

import "testing"

func TestLoopbackNeedsNoPermission(t *testing.T) {
	// The default, and the overwhelmingly common case.
	for _, addr := range []string{
		"127.0.0.1:7777", "localhost:7777", "[::1]:7777", "",
	} {
		if err := checkBind(addr, false); err != nil {
			t.Errorf("checkBind(%q) = %v, want it allowed", addr, err)
		}
	}
}

func TestBindingBeyondLoopbackIsRefusedUnlessAskedFor(t *testing.T) {
	// msr serves your source, your diffs and your review notes with no
	// authentication of any kind. Binding it to a network is a decision, and it
	// must not be one somebody makes by accident (ADR 0030).
	// `:7777` is in this list rather than the one above: it and `0.0.0.0:7777`
	// reach the same listener, and a guard that refuses one spelling while
	// allowing the other can be got around by typing less.
	for _, addr := range []string{"0.0.0.0:7777", "192.168.1.20:7777", "[::]:7777", ":7777"} {
		err := checkBind(addr, false)
		if err == nil {
			t.Errorf("checkBind(%q) allowed a non-loopback bind", addr)
			continue
		}
		// The message has to say what to do, or it is just an obstacle.
		if !contains(err.Error(), "--allow-remote") {
			t.Errorf("checkBind(%q) = %v, want it to name the flag", addr, err)
		}
		if !contains(err.Error(), "no authentication") {
			t.Errorf("checkBind(%q) = %v, want it to say why", addr, err)
		}
	}
}

func TestAskingForItAllowsIt(t *testing.T) {
	// Somebody in a container has a real reason, and this is their answer.
	if err := checkBind("0.0.0.0:7777", true); err != nil {
		t.Errorf("checkBind with --allow-remote = %v, want it allowed", err)
	}
}

func TestAnUnparseableAddressIsLeftToTheListener(t *testing.T) {
	// net.Listen gives a better message about a malformed address than this
	// could, and refusing here would replace it with a worse one.
	if err := checkBind("nonsense", false); err != nil {
		t.Errorf("checkBind(%q) = %v, want it passed through", "nonsense", err)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle ||
		len(needle) == 0 || indexOf(haystack, needle) >= 0)
}

func indexOf(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}

// A reviewer who asked for a phone on their own network then has to type an
// address into it.
func TestLANAddressesAreOnesAnotherDeviceCanReach(t *testing.T) {
	for _, url := range LANAddresses("7777") {
		if contains(url, "127.0.0.1") || contains(url, "::1") {
			t.Errorf("%s is this machine, which is the address they already have", url)
		}
		if contains(url, "169.254.") {
			t.Errorf("%s is link-local and is not routable from a phone", url)
		}
		if !contains(url, ":7777") {
			t.Errorf("%s does not carry the port", url)
		}
	}
}
