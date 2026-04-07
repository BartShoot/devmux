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
	name     string
	zigTarget string // zig -Dtarget value, empty for native
	libDir   string
	check    string // file to check if already built
}

var buildTargets = []buildTarget{
	{"linux", "", filepath.Join(ghosttyOut, "lib", "linux"), "libghostty-vt.so"},
	{"windows", "x86_64-windows", filepath.Join(ghosttyOut, "lib", "windows"), "ghostty-vt.lib"},
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
		fmt.Println("libghostty-vt already built for all targets, ensuring headers are synced...")
		copyLibs(buildTargets[0].libDir)
		return
	}

	fmt.Println("Building libghostty-vt...")
	needsCopy := false
	for _, t := range buildTargets {
		if _, err := os.Stat(filepath.Join(t.libDir, t.check)); err == nil {
			fmt.Printf("  %s: already built\n", t.name)
			needsCopy = true
			continue
		}
		fmt.Printf("  %s: building...\n", t.name)
		args := []string{"build", "-Demit-lib-vt=true", "-Doptimize=ReleaseFast"}
		if t.zigTarget != "" {
			args = append(args, "-Dtarget="+t.zigTarget)
		}
		cmd := exec.Command("zig", args...)
		cmd.Dir = ghosttySrc
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Printf("  %s: build failed (cross-compile not available), skipping: %v\n", t.name, err)
			continue
		}
		needsCopy = true
	}

	if needsCopy {
		// Just use the first available target's lib dir for logic, 
		// but copyLibs handles its own logic for headers and all lib types.
		copyLibs(buildTargets[0].libDir) 
	}
	fmt.Println("libghostty-vt setup completed")
}

func copyLibs(destLibDir string) {
	zigOutLib := filepath.Join(ghosttySrc, "zig-out", "lib")
	zigOutBin := filepath.Join(ghosttySrc, "zig-out", "bin")
	zigOutInclude := filepath.Join(ghosttySrc, "zig-out", "include")

	linuxLibDir := filepath.Join(ghosttyOut, "lib", "linux")
	windowsLibDir := filepath.Join(ghosttyOut, "lib", "windows")

	// Copy all library files (Linux .so/.a)
	if entries, _ := filepath.Glob(filepath.Join(zigOutLib, "libghostty-vt.*")); len(entries) > 0 {
		os.MkdirAll(linuxLibDir, 0o755)
		for _, src := range entries {
			copyFile(src, filepath.Join(linuxLibDir, filepath.Base(src)))
		}
	}
	// Windows .lib files
	if entries, _ := filepath.Glob(filepath.Join(zigOutLib, "ghostty-vt.*")); len(entries) > 0 {
		os.MkdirAll(windowsLibDir, 0o755)
		for _, src := range entries {
			copyFile(src, filepath.Join(windowsLibDir, filepath.Base(src)))
		}
	}
	// Windows DLLs
	if dlls, _ := filepath.Glob(filepath.Join(zigOutBin, "*.dll")); len(dlls) > 0 {
		os.MkdirAll(windowsLibDir, 0o755)
		for _, src := range dlls {
			copyFile(src, filepath.Join(windowsLibDir, filepath.Base(src)))
		}
	}

	// Copy headers (shared across targets, only once)
	if entries, err := os.ReadDir(zigOutInclude); err == nil {
		for _, e := range entries {
			src := filepath.Join(zigOutInclude, e.Name())
			dst := filepath.Join(includeDir, e.Name())
			// Remove existing to avoid nesting
			os.RemoveAll(dst)
			
			// Use xcopy on Windows, cp on others
			var cp *exec.Cmd
			if os.PathSeparator == '\\' {
				// /E - Copies directories and subdirectories, including empty ones.
				// /I - If destination does not exist and copying more than one file, 
				//      assumes that destination must be a directory.
				// /Y - Suppresses prompting to confirm you want to overwrite an existing destination file.
				cp = exec.Command("xcopy", "/E", "/I", "/Y", src, dst+"\\")
			} else {
				cp = exec.Command("cp", "-r", src, dst)
			}
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
