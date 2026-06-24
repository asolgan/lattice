//go:build ignore

// lint-conventions.go — static check for Lattice code conventions (CLAUDE.md
// "Code conventions"). Run via `make lint-conventions` or
//
//	go run ./scripts/lint-conventions.go [files...]
//
// With no file arguments it scans all git-tracked .go files. With --strict (or
// STRICT=1) it exits non-zero when any violation is found; otherwise it is
// advisory (prints findings, exits 0) so it can run as a non-blocking
// PostToolUse hook.
//
// Checks (v0 — highest-value, lowest-false-positive):
//   - History/changelog comments — git blame + the commit message are the
//     record. This is the single most-violated rule (CLAUDE.md).
//   - `asp.` key prefix in a Go string literal — aspects are 4-segment
//     vtx.<type>.<id>.<localName>, never an asp.* prefix (Contract #1).
//
// Markdown/docs are intentionally out of scope: they discuss the conventions
// (e.g. "never an asp.* prefix") and would false-positive. The 6-segment link
// check is deferred to v1 — naive matching collides with legitimate `"lnk."`
// key-builder prefix constants.
package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

var (
	historyComment = regexp.MustCompile(`//[ \t]*(Story [0-9]|Previously\b|Was:|Replaces\b|renamed from|moved from|formerly\b)`)
	aspPrefix      = regexp.MustCompile(`"asp\.`)
)

type finding struct {
	file string
	line int
	msg  string
}

func main() {
	strict := os.Getenv("STRICT") == "1"
	var files []string
	for _, a := range os.Args[1:] {
		if a == "--strict" {
			strict = true
			continue
		}
		files = append(files, a)
	}
	if len(files) == 0 {
		files = trackedGoFiles()
	}

	var findings []finding
	for _, f := range files {
		if !strings.HasSuffix(f, ".go") {
			continue
		}
		findings = append(findings, scanFile(f)...)
	}

	for _, fd := range findings {
		fmt.Printf("%s:%d: %s\n", fd.file, fd.line, fd.msg)
	}
	if len(findings) == 0 {
		fmt.Println("lint-conventions: 0 issues")
		return
	}
	fmt.Printf("lint-conventions: %d issue(s)\n", len(findings))
	if strict {
		os.Exit(1)
	}
}

func scanFile(path string) []finding {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []finding
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	ln := 0
	for sc.Scan() {
		ln++
		line := sc.Text()
		if historyComment.MatchString(line) {
			out = append(out, finding{path, ln, "history/changelog comment — git blame + the commit message are the record"})
		}
		if aspPrefix.MatchString(line) {
			out = append(out, finding{path, ln, "`asp.` key prefix — aspects are 4-segment vtx.<type>.<id>.<localName> (Contract #1)"})
		}
	}
	return out
}

func trackedGoFiles() []string {
	out, err := exec.Command("git", "ls-files", "*.go").Output()
	if err != nil {
		return nil
	}
	var files []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l != "" {
			files = append(files, l)
		}
	}
	return files
}
