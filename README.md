# DevMux: Scriptable Terminal Multiplexer & Process Manager

A tool that bridges the gap between terminal multiplexers (Zellij/Tmux) and process managers (Docker Compose).

## 1. Accomplishments (Phase 1 & 2 Complete)

- **YAML Configuration Engine**: Supports complex layouts with tabs and panes, including health check definitions (HTTP, TCP, Regex).
- **Daemon Process Manager**: 
    - Concurrent process spawning with PTY support (fallback to raw pipes on Windows).
    - Thread-safe, line-buffered scrollback capture for every pane.
    - Automated health monitoring loop (HTTP GET, TCP Dial, Regex on log stream).
- **TCP-Based CLI Client**: Lightweight binary that communicates with the daemon to query status and trigger restarts.
- **Verification Suite**: A set of Go-based test applications (`http-app`, `log-app`, `tcp-app`) to validate all health check types.

## 2. Technical Challenges & Lessons Learned

- **Cross-Platform PTYs**: `creack/pty` is unsupported on Windows. We implemented a robust fallback to `exec.Command` with `StdoutPipe` and `Stderr` redirection to ensure the daemon works across OSs.
- **Inter-Process Communication (IPC)**: Unix Domain Sockets proved inconsistent on Windows environments. We pivoted to a **TCP Socket (localhost:8888)** for the Daemon-CLI communication, ensuring stability and easier cross-platform TUI development later.
- **Port Management**: Identified that orphaned processes (e.g., Spring Boot on 8080) can block restarts. The daemon's restart logic now explicitly kills the process tree before spawning new instances.
- **Log Buffering**: Regex health checks required a specialized `LogBuffer` that explicitly handles line endings (`\n`) to ensure pattern matching doesn't fail due to partial buffer writes.

## 3. Pillar Architecture

- **Daemon (The Brain)**: Owns process lifecycles and health check timers.
- **TUI Frontend (The Viewer)**: Attaches to the daemon via TCP to display tabs/panes.
- **CLI Client (The Automator)**: Lightweight tool to send instructions and wait for state changes.

## 4. Current Roadmap: Phase 3 (TUI)

- [ ] **TCP Log Streaming**: Implement a protocol to stream scrollback buffers from the Daemon to the TUI.
- [ ] **Layout Rendering**: Use `tview` (Go) to create a static layout based on the YAML.
- [ ] **Interactive Input**: Map keystrokes in the active TUI pane to write directly to the Daemon's stdin/PTY.
- [ ] **Terminal Emulation**: Address ANSI escape code parsing for the TUI (Windows raw pipe compatibility).

## 5. Usage (Testing the MVP)

1. **Start the Daemon**:
   ```bash
   go run cmd/devmuxd/main.go
   ```
2. **Check Status**:
   ```bash
   go run cmd/devmux/main.go status
   ```
3. **Restart a Process**:
   ```bash
   go run cmd/devmux/main.go restart Frontend
   ```
