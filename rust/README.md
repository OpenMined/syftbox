# SyftBox Rust Client

This directory contains the Rust implementation of the SyftBox client. It
currently implements a lightweight sync daemon that mirrors the behaviour
required by the devstack test suite: it watches your workspace and propagates
files between datasites for all clients in the same devstack root.

## Running

Build and run the daemon:

```
cargo run --release -- -c /path/to/config.json daemon --http-addr 127.0.0.1:7938
```

The Just target `sbdev-test-rust` builds the Rust client and runs the devstack
tests with `SBDEV_CLIENT_BIN` pointed at the resulting binary.
