# ADR 0005: Daemon process model and RPC protocol

Status: accepted, 2026-09-29.

## Decision

- One daemon per `CREW_HOME`, guarded by `flock` on `crew.lock`. The pid file is informational; the lock is the truth.
- One goroutine owns state. Events and queries reach it through channels; nothing in it blocks on disk or on a client.
- Newline-delimited JSON on a Unix socket, every message versioned with `"v":1`, requests matched to responses by a client-chosen `id`. `subscribe` returns a full state snapshot with a `seq`, then ordered diffs on the same `id`.
- A subscriber that falls 1024 messages behind is disconnected. It resubscribes and gets a fresh snapshot.
- The client auto-starts the daemon through a callback, because `rpc` may not import `os/exec`.
- Commands that run unattended (`crew daemon status`, `crew notify stream`) never start it. A daemon inherits the environment of whatever started it, and theirs is often a bare `ssh` or launchd one (#152).

## Why

- `flock` is released by the kernel when the process dies, so `kill -9` never leaves a lock that needs manual cleanup.
- Snapshot plus seq-numbered diffs lets a client verify it missed nothing and recover by resubscribing.
- Dropping a slow subscriber keeps the loop within the hook-to-sidebar budget; blocking on one would stall every client.

## Rejected

- **gRPC or JSON-RPC 2.0:** more machinery than a few methods need; hooks and the nvim plugin want something `nc`-able.
- **pid file liveness checks:** racy and wrong after pid reuse.
