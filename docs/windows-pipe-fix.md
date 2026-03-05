# The Bug: Why Unpausing on the Host Didn't Resume the Client

Fixed in v1.3.0 (`ed72a76`).

## The Setup

When LocalSync runs, the client has two concurrent loops talking to MPV over a single named pipe (`\\.\pipe\mpvsync`):

1. **Read loop** — A `bufio.Scanner` goroutine constantly reading IPC events from MPV (time-pos updates, pause state changes, etc.)
2. **Write path** — The WS→IPC goroutine writing commands to MPV (like `set_property pause false`) whenever the host sends a sync message

Both loops share the same pipe connection — one reads from it, the other writes to it.

## How Windows Named Pipes Work Internally

A named pipe on Windows is a kernel object managed by the Named Pipe File System (NPFS) driver. When you call `CreateFile` (which is what Go's `os.OpenFile` does under the hood) to open a pipe, two flags matter enormously:

- **Without `FILE_FLAG_OVERLAPPED`** (synchronous mode) — The kernel associates the file handle with a single I/O completion state. All operations on that handle are **serialized by the kernel**. If a `ReadFile` is in progress, a `WriteFile` on the same handle will **block in kernel mode** until the read completes. The thread calling Write literally cannot proceed — it's parked in the kernel waiting for its turn.

- **With `FILE_FLAG_OVERLAPPED`** (asynchronous/overlapped mode) — Each I/O operation gets its own `OVERLAPPED` structure and is tracked independently via I/O Completion Ports (IOCP). Reads and writes are fully concurrent — the kernel manages them as separate operations that don't block each other.

This is fundamentally different from Unix, where file descriptors to pipes/sockets are just entries in the process's fd table pointing to a kernel buffer. Unix pipes have **separate read and write buffers** in the kernel, so `read()` and `write()` on the same fd never block each other (they contend on different locks). A Unix `read()` blocks only when the read buffer is empty; a `write()` blocks only when the write buffer is full. They're independent operations.

## What Was Happening

The old code opened the pipe like this:

```go
f, err := os.OpenFile(path, os.O_RDWR, 0)
```

This calls `CreateFile` **without** `FILE_FLAG_OVERLAPPED`. The handle is synchronous.

Here's the timeline when the host unpauses:

1. **MPV is paused** — it produces no IPC output (no time-pos events, nothing)
2. The read loop calls `Read()` on the pipe — since there's nothing to read, it **blocks in the kernel**, waiting for data
3. Host unpauses → server sends `{"event":"pause","state":false}` over WebSocket to client
4. Client's WS→IPC goroutine receives it, tries to `Write()` the unpause command to MPV
5. **The write blocks** — the kernel won't let it proceed because there's already a pending read on this synchronous handle
6. **Deadlock**: the read is waiting for MPV to send data, but MPV won't unpause (and thus won't send data) until it receives the write that's blocked behind the read

When MPV was **playing**, it flooded time-pos events every ~42ms. So the read loop would complete quickly, giving the write a brief window to slip through. But even then, the write could wait up to 3 seconds for a gap — you could see this in the logs:

```
18:18:41 — ipcWriteErr called with "set_property pause false"
18:18:44 — "pause command written OK"   ← 3 second delay!
```

When MPV was **paused**, there were zero events, so reads blocked indefinitely. The write would never complete. The client was permanently stuck.

## Why This Wasn't Obvious

- On **Unix/macOS**, this bug doesn't exist. Unix sockets have independent read/write paths — `read()` and `write()` never block each other on the same fd
- The `pipeConn` wrapper implemented `net.Conn`, so it *looked* like a proper bidirectional connection with concurrent support
- When MPV was playing, writes mostly worked (just with latency), so it seemed like a timing issue rather than a fundamental I/O serialization problem
- The Go runtime doesn't warn you that `os.OpenFile` on a Windows pipe creates a synchronous handle

## The Fix

```go
// Before: synchronous handle, serialized I/O
f, err := os.OpenFile(path, os.O_RDWR, 0)
return &pipeConn{f: f}, nil

// After: overlapped handle via IOCP, concurrent I/O
return winio.DialPipe(path, nil)
```

`go-winio` (Microsoft's own Go library for Windows I/O) calls `CreateFile` with `FILE_FLAG_OVERLAPPED` and wires the handle into Go's runtime network poller via IOCP. This means:

- Reads and writes are tracked as **independent I/O operations** with separate `OVERLAPPED` structures
- A pending read no longer blocks writes (and vice versa)
- It returns a proper `net.Conn` with working deadlines, so the `pipeConn` wrapper and all its no-op methods were deleted entirely

## IOCP in Brief

I/O Completion Ports are Windows' high-performance async I/O mechanism. Instead of blocking a thread per operation:

1. You associate a file handle with a completion port
2. You submit I/O operations (read/write) that return immediately
3. Worker threads call `GetQueuedCompletionStatus` to pick up completed operations
4. Go's runtime poller integrates with this, so goroutines park/wake efficiently without burning OS threads

This is the Windows equivalent of `epoll` (Linux) or `kqueue` (macOS) — it's how Go's `net` package handles sockets natively. By using `go-winio`, the named pipe now participates in the same system, getting the same concurrency guarantees as a TCP connection.
