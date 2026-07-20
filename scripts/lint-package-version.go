//go:build ignore

// lint-package-version.go — a packages/ content edit must bump that package's
// manifest version, or running stacks never see it: plain install no-ops an
// unchanged version, so a permission/lens/DDL change silently fails to reach
// any live stack (docs/components/_packages.md "Refresh / upgrade").
//
// Run via `make lint-package-version` or
//
//	go run ./scripts/lint-package-version.go
//
// Modes:
//   - Local (no DIFF_BASE): compares the working tree + index (and untracked
//     files under packages/) against HEAD — run it before committing a
//     packages/ change.
//   - Range (DIFF_BASE=<sha>, set by CI): compares DIFF_BASE..DIFF_HEAD
//     (DIFF_HEAD defaults to HEAD). CI passes the pushed range (push:
//     github.event.before; PR: the base sha). A base missing from a shallow
//     clone is fetched by SHA; if it still can't be resolved the gate skips
//     with a notice rather than failing the build on git plumbing.
//
// A package's "content" is every file under packages/<name>/ except *_test.go
// and *.md — the files that shape what install writes. The version check reads
// manifest.yaml's `version:` value; package.go's Definition.Version is pinned
// to it by every package's TestPackage_ManifestMatchesDefinition, so one
// bumped value implies both.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
)

var versionRe = regexp.MustCompile(`(?m)^version:\s*"?([^"\s#]+)`)

func main() {
	base := strings.TrimSpace(os.Getenv("DIFF_BASE"))
	head := strings.TrimSpace(os.Getenv("DIFF_HEAD"))
	if head == "" {
		head = "HEAD"
	}

	var changed []string
	rangeMode := base != "" && !isZeroSHA(base)
	if rangeMode {
		if !ensureCommit(base) {
			fmt.Printf("lint-package-version: base %s unavailable (shallow clone, fetch failed) — skipping.\n", base)
			return
		}
		changed = gitLines("diff", "--name-only", base, head)
	} else {
		changed = gitLines("diff", "--name-only", "HEAD")
		changed = append(changed, gitLines("ls-files", "--others", "--exclude-standard", "packages/")...)
	}

	contentChanged := map[string]int{}
	for _, path := range changed {
		pkg, ok := packageOf(path)
		if !ok || strings.HasSuffix(path, "_test.go") || strings.HasSuffix(path, ".md") {
			continue
		}
		contentChanged[pkg]++
	}
	if len(contentChanged) == 0 {
		fmt.Println("lint-package-version: clean — no packages/ content changes.")
		return
	}

	baseRef := "HEAD"
	if rangeMode {
		baseRef = base
	}
	pkgs := make([]string, 0, len(contentChanged))
	for pkg := range contentChanged {
		pkgs = append(pkgs, pkg)
	}
	sort.Strings(pkgs)

	violations := 0
	for _, pkg := range pkgs {
		manifest := "packages/" + pkg + "/manifest.yaml"
		baseVer, baseOK := versionAt(baseRef, manifest)
		if !baseOK {
			// New package in this range — any version it declares is fresh.
			continue
		}
		var headVer string
		var headOK bool
		if rangeMode {
			headVer, headOK = versionAt(head, manifest)
		} else {
			headVer, headOK = versionOnDisk(manifest)
		}
		if !headOK {
			if len(gitLines("ls-files", "packages/"+pkg+"/")) == 0 {
				continue // package deleted in this range — nothing to install
			}
			fmt.Printf("lint-package-version: packages/%s content changed but it has no readable manifest.yaml version\n", pkg)
			violations++
			continue
		}
		if headVer == baseVer {
			fmt.Printf("lint-package-version: packages/%s content changed (%d file(s)) but manifest.yaml version is unchanged at %s\n", pkg, contentChanged[pkg], headVer)
			fmt.Printf("  bump %s `version:` (+ package.go Definition.Version — parity is test-pinned);\n", manifest)
			fmt.Printf("  an unchanged version no-ops plain install, so this change never reaches a running stack.\n")
			violations++
		}
	}
	if violations > 0 {
		fmt.Printf("lint-package-version: %d package(s) need a version bump.\n", violations)
		os.Exit(1)
	}
	fmt.Printf("lint-package-version: clean — %d changed package(s), all version-bumped (or new).\n", len(pkgs))
}

// packageOf extracts the package name from a packages/<name>/... path.
func packageOf(path string) (string, bool) {
	rest, ok := strings.CutPrefix(path, "packages/")
	if !ok {
		return "", false
	}
	name, _, found := strings.Cut(rest, "/")
	if !found || name == "" {
		return "", false
	}
	return name, true
}

func isZeroSHA(s string) bool {
	return strings.Trim(s, "0") == ""
}

// ensureCommit makes sure the base SHA is resolvable, fetching it by SHA into
// a shallow clone if needed.
func ensureCommit(sha string) bool {
	if exec.Command("git", "cat-file", "-e", sha+"^{commit}").Run() == nil {
		return true
	}
	_ = exec.Command("git", "fetch", "--depth=1", "origin", sha).Run()
	return exec.Command("git", "cat-file", "-e", sha+"^{commit}").Run() == nil
}

// versionAt reads the manifest's version value at a git ref; ok=false when the
// file does not exist there or carries no version line.
func versionAt(ref, path string) (string, bool) {
	out, err := exec.Command("git", "show", ref+":"+path).Output()
	if err != nil {
		return "", false
	}
	return parseVersion(string(out))
}

// versionOnDisk reads the manifest's version value from the working tree.
func versionOnDisk(path string) (string, bool) {
	out, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return parseVersion(string(out))
}

func parseVersion(src string) (string, bool) {
	m := versionRe.FindStringSubmatch(src)
	if m == nil {
		return "", false
	}
	return m[1], true
}

func gitLines(args ...string) []string {
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "lint-package-version: git %s: %v\n", strings.Join(args, " "), err)
		os.Exit(2)
	}
	var lines []string
	for _, ln := range strings.Split(string(out), "\n") {
		if ln = strings.TrimSpace(ln); ln != "" {
			lines = append(lines, ln)
		}
	}
	return lines
}
