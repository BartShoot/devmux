//go:build ignore

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

var (
	ghosttySrc = filepath.Join("third_party", "ghostty-src")
	ghosttyOut = filepath.Join("third_party", "ghostty")
	includeDir = filepath.Join(ghosttyOut, "include")
)

type buildTarget struct {
	name   string
	libDir string
	check  string // file to check if already built
}

var buildTargets = []buildTarget{
	{"linux", filepath.Join(ghosttyOut, "lib", "linux"), "libghostty-vt.so"},
	{"windows", filepath.Join(ghosttyOut, "lib", "windows"), "ghostty-vt.lib"},
}

func main() {
	os.MkdirAll(includeDir, 0o755)

	allBuilt := true
	for _, t := range buildTargets {
		os.MkdirAll(t.libDir, 0o755)
		if _, err := os.Stat(filepath.Join(t.libDir, t.check)); err != nil {
			allBuilt = false
		}
	}
	if allBuilt {
		fmt.Println("libghostty-vt already built for all targets, skipping")
		return
	}

	fmt.Println("Building libghostty-vt...")
	for _, t := range buildTargets {
		if _, err := os.Stat(filepath.Join(t.libDir, t.check)); err == nil {
			fmt.Printf("  %s: already built, skipping\n", t.name)
			continue
		}
		fmt.Printf("  %s: building...\n", t.name)
		args := []string{"build", "-Demit-lib-vt=true", "-Doptimize=ReleaseFast"}
		cmd := exec.Command("zig", args...)
		cmd.Dir = ghosttySrc
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Printf("  %s: build failed (cross-compile not available), skipping: %v\n", t.name, err)
			continue
		}
		copyLibs(t.libDir)
	}
	fmt.Println("libghostty-vt built successfully")
}

func copyLibs(destLibDir string) {
	zigOutLib := filepath.Join(ghosttySrc, "zig-out", "lib")
	zigOutBin := filepath.Join(ghosttySrc, "zig-out", "bin")
	zigOutInclude := filepath.Join(ghosttySrc, "zig-out", "include")

	// Copy all library files (Linux .so/.a)
	if entries, _ := filepath.Glob(filepath.Join(zigOutLib, "libghostty-vt.*")); len(entries) > 0 {
		for _, src := range entries {
			copyFile(src, filepath.Join(destLibDir, filepath.Base(src)))
		}
	}
	// Windows .lib files
	if entries, _ := filepath.Glob(filepath.Join(zigOutLib, "ghostty-vt.*")); len(entries) > 0 {
		for _, src := range entries {
			copyFile(src, filepath.Join(destLibDir, filepath.Base(src)))
		}
	}
	// Windows DLLs
	if dlls, _ := filepath.Glob(filepath.Join(zigOutBin, "*.dll")); len(dlls) > 0 {
		for _, src := range dlls {
			copyFile(src, filepath.Join(destLibDir, filepath.Base(src)))
		}
	}

	// Copy headers (shared across targets)
	if entries, err := os.ReadDir(zigOutInclude); err == nil {
		for _, e := range entries {
			src := filepath.Join(zigOutInclude, e.Name())
			dst := filepath.Join(includeDir, e.Name())
			cp := exec.Command("cp", "-r", src, dst)
			cp.Run()
		}
	}
}

func copyFile(src, dst string) {
	data, err := os.ReadFile(src)
	if err != nil {
		return
	}
	info, _ := os.Stat(src)
	os.WriteFile(dst, data, info.Mode())
}
