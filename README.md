# DevMux: Scriptable Terminal Multiplexer & Process Manager

A tool that bridges the gap between terminal multiplexers (Zellij/Tmux) and process managers (Docker Compose).

## 1. Accomplishments (Phase 1 & 2 Complete)

- **YAML Configuration Engine**: Supports complex layouts with tabs and panes, including health check definitions (HTTP, TCP, Regex).
- **Daemon Process Manager**: 
    - Concurrent process spawning with PTY support (fallback to raw pipes on Windows).
    - Thread-safe **Circular (Ring) Buffer** for scrollback capture, ensuring constant memory usage.
    - Automated health monitoring loop (HTTP GET, TCP Dial, Regex on log stream).
- **TCP-Based CLI Client**: Lightweight binary that communicates with the daemon to query status and trigger restarts.
- **Verification Suite**: A set of Go-based test applications (`http-app`, `log-app`, `tcp-app`) to validate all health check types.

## 2. Technical Challenges & Lessons Learned

- **Cross-Platform PTYs**: `creack/pty` is unsupported on Windows. We implemented a robust fallback to `exec.Command` with `StdoutPipe` and `Stderr` redirection to ensure the daemon works across OSs.
- **Inter-Process Communication (IPC)**: Unix Domain Sockets proved inconsistent on Windows environments. We pivoted to a **TCP Socket (localhost:8888)** for the Daemon-CLI communication, ensuring stability and easier cross-platform TUI development later.
- **Memory Efficiency**: Implemented a **Circular (Ring) Buffer** for logs. Instead of growing indefinitely, each pane stores a fixed number of lines (default 1000) using a rolling `[]byte` strategy, ensuring constant memory footprint regardless of log volume.
- **Port Management**: Identified that orphaned processes (e.g., Spring Boot on 8080) can block restarts. The daemon's restart logic now explicitly kills the process tree using `taskkill` on Windows before spawning new instances.

## 3. Pillar Architecture

- **Daemon (The Brain)**: Owns process lifecycles and health check timers.
- **TUI Frontend (The Viewer)**: Attaches to the daemon via TCP to display tabs/panes.
- **CLI Client (The Automator)**: Lightweight tool to send instructions and wait for state changes.

## 4. Current Roadmap: Phase 3 (TUI)

- [x] **Layout Rendering**: Use `tview` (Go) to create a static layout based on the YAML.
- [x] **Smart Scroll**: Implement auto-scrolling by default, with manual override when inspecting history.
- [ ] **Interactive Input**: Map keystrokes in the active TUI pane to write directly to the Daemon's stdin/PTY.
- [ ] **Terminal Emulation**: Address ANSI escape code parsing for the TUI (Windows raw pipe compatibility).

## 5. Usage (Testing the MVP)

### Running from Source:
1. **Start the Stack**: `go run cmd/devmux/main.go start`
2. **Visualize**: `go run cmd/devmux/main.go ui`
3. **Stop**: `go run cmd/devmux/main.go stop`

### Building for Distribution:
To create standalone binaries, run:
```powershell
go build -o bin/devmuxd.exe ./cmd/devmuxd
go build -o bin/devmux.exe ./cmd/devmux
```
The resulting executables in `bin/` can be shared and run without needing the Go toolchain installed.
