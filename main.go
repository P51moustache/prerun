package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const version = "0.1.0"

func main() {
	if len(os.Args) < 2 || os.Args[1] == "-h" || os.Args[1] == "--help" || os.Args[1] == "help" {
		usage()
		return
	}
	switch os.Args[1] {
	case "version", "--version":
		fmt.Println("prerun", version)
	case "run":
		if err := runCmd(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "\x1b[31m✗ %v\x1b[0m\n", err)
			os.Exit(1)
		}
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Print(`prerun: run and debug your GitLab CI pipeline locally, before you push it

usage:
  prerun run [pipeline.yml] [flags]     defaults to .gitlab-ci.yml

flags:
  --break job[:step]    pause before a step (1-based) and open a shell inside the job container
  --break-exec "cmd"    at the breakpoint, run this command instead of a shell (for scripting)
  --default-image IMG   image for jobs that don't set one           (default: alpine:latest)
  --artifacts DIR       host directory for inter-job artifacts      (default: .prerun/artifacts)
  --docker BIN          container CLI to use, e.g. podman           (default: docker)

examples:
  prerun run
  prerun run ci/pipeline.yml --break test:2
  prerun run --break-exec 'env | sort'
`)
}

func runCmd(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	brk := fs.String("break", "", "")
	brkExec := fs.String("break-exec", "", "")
	defImage := fs.String("default-image", "alpine:latest", "")
	artifacts := fs.String("artifacts", filepath.Join(".prerun", "artifacts"), "")
	docker := fs.String("docker", "docker", "")
	// Go's flag package stops at the first positional argument, which would
	// silently ignore flags in `prerun run pipeline.yml --break x`. Re-parse
	// around positionals so flags and the file path can appear in any order.
	file := ".gitlab-ci.yml"
	fileSet := false
	rest := args
	for {
		if err := fs.Parse(rest); err != nil {
			return err
		}
		if fs.NArg() == 0 {
			break
		}
		if fileSet {
			return fmt.Errorf("unexpected argument %q (pipeline file already given: %s)", fs.Arg(0), file)
		}
		file, fileSet = fs.Arg(0), true
		rest = fs.Args()[1:]
	}

	p, unsupported, err := parsePipeline(file)
	if err != nil {
		return err
	}
	if len(unsupported) > 0 {
		fmt.Fprintf(os.Stderr, "\x1b[33m⚠ ignoring unsupported top-level keys: %s\x1b[0m\n", strings.Join(unsupported, ", "))
		fmt.Fprintf(os.Stderr, "\x1b[33m  prerun v%s supports a documented subset of GitLab CI — see README\x1b[0m\n", version)
	}

	if err := os.MkdirAll(*artifacts, 0o755); err != nil {
		return err
	}
	return NewRunner(p, RunOptions{
		DefaultImage: *defImage,
		ArtifactDir:  *artifacts,
		Breakpoint:   *brk,
		BreakExec:    *brkExec,
		Docker:       *docker,
	}).Run()
}
