//go:build mage

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

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
	{"windows", "amd64"},
	{"windows", "arm64"},
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

// Version prints the current version based on git tags.
func Version() error {
	v, err := currentVersion()
	if err != nil {
		return err
	}
	fmt.Println(v)
	return nil
}

// Tag creates and pushes a new semver tag. Accepts "patch", "minor", or "major".
// Defaults to "patch" if no argument is given.
//
// Usage:
//
//	mage tag          # bump patch: v0.1.0 → v0.1.1
//	mage tag:patch    # same as above
//	mage tag:minor    # bump minor: v0.1.1 → v0.2.0
//	mage tag:major    # bump major: v0.2.0 → v1.0.0
type Tag mg.Namespace

// Patch bumps the patch version (v0.1.0 → v0.1.1) and pushes the tag.
func (Tag) Patch() error { return bumpAndPush("patch") }

// Minor bumps the minor version (v0.1.0 → v0.2.0) and pushes the tag.
func (Tag) Minor() error { return bumpAndPush("minor") }

// Major bumps the major version (v0.1.0 → v1.0.0) and pushes the tag.
func (Tag) Major() error { return bumpAndPush("major") }

func currentVersion() (string, error) {
	out, err := sh.Output("git", "describe", "--tags", "--abbrev=0")
	if err != nil {
		// No tags yet.
		return "v0.0.0", nil
	}
	return strings.TrimSpace(out), nil
}

func bumpAndPush(part string) error {
	cur, err := currentVersion()
	if err != nil {
		return err
	}

	next, err := bumpVersion(cur, part)
	if err != nil {
		return err
	}

	// Verify working tree is clean.
	if err := sh.Run("git", "diff", "--quiet", "HEAD"); err != nil {
		return fmt.Errorf("working tree is dirty — commit or stash changes first")
	}

	fmt.Printf("Tagging %s → %s\n", cur, next)

	if err := sh.RunV("git", "tag", "-a", next, "-m", "Release "+next); err != nil {
		return fmt.Errorf("git tag: %w", err)
	}
	if err := sh.RunV("git", "push", "origin", next); err != nil {
		return fmt.Errorf("git push: %w", err)
	}

	fmt.Printf("Pushed %s — GitHub Actions will create the release.\n", next)
	return nil
}

func bumpVersion(version, part string) (string, error) {
	v := strings.TrimPrefix(version, "v")
	parts := strings.Split(v, ".")

	if len(parts) != 3 {
		return "", fmt.Errorf("invalid version %q: expected vMAJOR.MINOR.PATCH", version)
	}

	var major, minor, patch int
	if _, err := fmt.Sscanf(parts[0], "%d", &major); err != nil {
		return "", fmt.Errorf("parse major: %w", err)
	}
	if _, err := fmt.Sscanf(parts[1], "%d", &minor); err != nil {
		return "", fmt.Errorf("parse minor: %w", err)
	}
	if _, err := fmt.Sscanf(parts[2], "%d", &patch); err != nil {
		return "", fmt.Errorf("parse patch: %w", err)
	}

	switch part {
	case "major":
		major++
		minor = 0
		patch = 0
	case "minor":
		minor++
		patch = 0
	case "patch":
		patch++
	default:
		return "", fmt.Errorf("unknown bump part %q: use major, minor, or patch", part)
	}

	return fmt.Sprintf("v%d.%d.%d", major, minor, patch), nil
}
