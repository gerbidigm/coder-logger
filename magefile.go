//go:build mage

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/sh"
)

var Default = Build

type target struct {
	GOOS   string
	GOARCH string
}

var targets = []target{
	{"linux", "amd64"},
	{"linux", "arm64"},
	{"darwin", "arm64"},
}

// Build cross-compiles coder-logger for all targets into dist/.
func Build() error {
	mg.Deps(Clean)

	for _, t := range targets {
		if err := buildTarget(t); err != nil {
			return err
		}
	}
	return nil
}

func buildTarget(t target) error {
	bin := fmt.Sprintf("coder-logger_%s_%s", t.GOOS, t.GOARCH)
	if t.GOOS == "windows" {
		bin += ".exe"
	}
	out := filepath.Join("dist", bin)
	fmt.Printf("Building %s/%s → %s\n", t.GOOS, t.GOARCH, out)

	env := map[string]string{
		"GOOS":        t.GOOS,
		"GOARCH":      t.GOARCH,
		"CGO_ENABLED": "0",
	}
	return sh.RunWithV(env, "go", "build", "-o", out, "./cmd/coder-logger")
}

// Clean removes the dist/ directory.
func Clean() error {
	fmt.Println("Cleaning dist/")
	return os.RemoveAll("dist")
}

// BuildLocal builds for the current platform into dist/.
func BuildLocal() error {
	t := target{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}
	return buildTarget(t)
}

// Lint runs go vet on all packages.
func Lint() error {
	return sh.RunV("go", "vet", "./...")
}

// Test runs all tests.
func Test() error {
	return sh.RunV("go", "test", "./...")
}

// check that mage binary is available (for CI bootstrapping).
func ensureMage() error {
	_, err := exec.LookPath("mage")
	return err
}
