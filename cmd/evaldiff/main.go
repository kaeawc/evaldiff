// Command evaldiff statically answers "which evals will this PR break?"
//
// Subcommands:
//
//	evaldiff index <dir>          Build the behavior index for a source tree
//	                              and print it as JSON.
//	evaldiff diff <base> <head>   (stub) Print evals affected between two
//	                              revisions.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/kaeawc/evaldiff/internal/diff"
	"github.com/kaeawc/evaldiff/internal/index"
	"github.com/kaeawc/evaldiff/internal/vfs"
)

var version = "dev"

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "evaldiff:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		printUsage(stderr)
		return errors.New("no command given")
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "index":
		return runIndex(rest, stdout)
	case "diff":
		return runDiff(rest, stdout)
	case "-h", "--help", "help":
		printUsage(stdout)
		return nil
	case "-v", "--version", "version":
		fmt.Fprintln(stdout, version)
		return nil
	default:
		printUsage(stderr)
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func runIndex(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("index", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // errors are returned, not printed
	hashOnly := fs.Bool("hash", false, "print only the index hash, not the full JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) != 1 {
		return errors.New("usage: evaldiff index [--hash] <dir>")
	}
	dir := rest[0]
	idx, err := index.Build(context.Background(), vfs.OS{}, dir)
	if err != nil {
		return err
	}
	if *hashOnly {
		h, err := idx.Hash()
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, h)
		return nil
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(idx)
}

func runDiff(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("diff", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) != 2 {
		return errors.New("usage: evaldiff diff <baseDir> <headDir>")
	}
	baseDir, headDir := rest[0], rest[1]
	ctx := context.Background()
	base, err := index.Build(ctx, vfs.OS{}, baseDir)
	if err != nil {
		return fmt.Errorf("build base index: %w", err)
	}
	head, err := index.Build(ctx, vfs.OS{}, headDir)
	if err != nil {
		return fmt.Errorf("build head index: %w", err)
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(diff.Diff(base, head))
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, `usage: evaldiff <command> [args]

commands:
  index [--hash] <dir>   Build behavior index for a source tree (JSON to stdout).
                         --hash prints only the content-addressable hash.
  diff  <baseDir> <headDir>
                         Diff two source trees, print structured changeset (JSON to stdout).
  version                Print version and exit.
  help                   Show this message.`)
}
