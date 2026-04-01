//go:build ignore

package main

import (
	"fmt"
	"os"
	"os/exec"
)

func main() {
	// If we are in GitHub Actions (Linux), we need to build BOTH targets
	// so GoReleaser can find the Windows .lib/.dll when cross-compiling.
	// isCI := os.Getenv("GITHUB_ACTIONS") == "true"

	// if runtime.GOOS == "windows" {
	// 	run("powershell", "-ExecutionPolicy", "Bypass", "-File", "./make.ps1", "ghostty-build")
	// } else {
	// 	if isCI {
	// In CI, we explicitly build both
	fmt.Println("CI detected: Building ghostty for both Linux and Windows...")
	run("zig", "build", "-C", "third_party/ghostty-src", "-Demit-lib-vt=true", "-Doptimize=ReleaseFast", "-Dtarget=x86_64-linux")
	run("zig", "build", "-C", "third_party/ghostty-src", "-Demit-lib-vt=true", "-Doptimize=ReleaseFast", "-Dtarget=x86_64-windows")
	// } else {
	// 	run("make", "ghostty-build")
	// }
	// }
}

func run(name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Printf("Error running %s: %v\n", name, err)
		os.Exit(1)
	}
}
