package main

import "testing"

func TestParseStartFlagsSkipClientDaemons(t *testing.T) {
	opts, err := parseStartFlags([]string{
		"--path", "sandbox",
		"--client", "client1@sandbox.local",
		"--skip-client-daemons",
	})
	if err != nil {
		t.Fatalf("parseStartFlags err: %v", err)
	}
	if !opts.skipClientDaemons {
		t.Fatalf("expected skipClientDaemons=true")
	}
	if len(opts.clients) != 1 || opts.clients[0] != "client1@sandbox.local" {
		t.Fatalf("unexpected clients: %#v", opts.clients)
	}
}

