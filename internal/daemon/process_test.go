package daemon

import (
	"context"
	"testing"
	"time"

	"devmux/internal/config"
)

func TestProcessManager_StartProcess(t *testing.T) {
	pm := NewProcessManager()

	// Use a command that exits quickly
	err := pm.StartProcess("test-proc", "echo hello", "", config.HealthCheck{})
	if err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	pm.mu.Lock()
	p, exists := pm.processes["test-proc"]
	pm.mu.Unlock()

	if !exists {
		t.Fatal("Process not found in manager")
	}

	if p.Name != "test-proc" {
		t.Errorf("Expected name 'test-proc', got '%s'", p.Name)
	}

	// Wait for it to finish and check Running state
	time.Sleep(500 * time.Millisecond)
	pm.mu.Lock()
	running := p.Running
	pm.mu.Unlock()

	if running {
		t.Error("Expected process to be finished, but it is still running")
	}
}

func TestProcessManager_RestartProcess(t *testing.T) {
	pm := NewProcessManager()

	// Use a command that stays alive for a bit (sleep is simpler on Linux)
	err := pm.StartProcess("restart-proc", "sleep 5", "", config.HealthCheck{})
	if err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	// Verify it's running
	pm.mu.Lock()
	p := pm.processes["restart-proc"]
	pm.mu.Unlock()
	
	if !p.Running {
		t.Fatal("Process should be running")
	}

	// Trigger restart
	err = pm.RestartProcess("restart-proc")
	if err != nil {
		t.Fatalf("Restart failed: %v", err)
	}

	// Verify new process is tracked
	pm.mu.Lock()
	newP := pm.processes["restart-proc"]
	pm.mu.Unlock()

	if newP == p {
		t.Error("Restart should have created a new ManagedProcess instance")
	}

	if !newP.Running {
		t.Error("Restarted process should be running")
	}

	// Cleanup
	pm.StopAll()
}

func TestProcessManager_HealthCheckIntegration(t *testing.T) {
	pm := NewProcessManager()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start health check loop
	go pm.RunHealthChecks(ctx)

	// Start a process with a regex health check
	err := pm.StartProcess("health-proc", "echo READY", "", config.HealthCheck{
		Type:    "regex",
		Pattern: "READY",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Give it time to run the health check
	time.Sleep(2 * time.Second)

	pm.mu.Lock()
	p := pm.processes["health-proc"]
	status := p.Status
	lines := p.Buffer.GetLines()
	pm.mu.Unlock()

	if status != StatusHealthy {
		t.Errorf("Expected status 'Healthy', got '%s'. Buffer: %v", status, lines)
	}
}
