// Command evaldiff statically answers "which evals will this PR break?"
//
// Headline form:
//
//	evaldiff <baseDir> <headDir>     Build behavior + coverage indexes for
//	                                 head, diff against base, intersect,
//	                                 rank, and print the EvalsAtRisk JSON.
//	                                 Equivalent to `evaldiff risk ...`.
//
// Subcommands:
//
//	evaldiff risk <baseDir> <headDir>   Same as the headline form.
//	evaldiff index <dir>                Build the behavior index for a source
//	                                    tree and print it as JSON.
//	evaldiff coverage <dir>             Build the eval-coverage index (test
//	                                    catalog) for a source tree.
//	evaldiff diff <baseDir> <headDir>   Diff two source trees, emit the
//	                                    structured changeset.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/kaeawc/evaldiff/internal/coverage"
	"github.com/kaeawc/evaldiff/internal/diff"
	"github.com/kaeawc/evaldiff/internal/index"
	evaldiffio "github.com/kaeawc/evaldiff/internal/io"
	"github.com/kaeawc/evaldiff/internal/rank"
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
	case "coverage":
		return runCoverage(rest, stdout)
	case "diff":
		return runDiff(rest, stdout)
	case "risk":
		return runRisk(rest, stdout)
	case "-h", "--help", "help":
		printUsage(stdout)
		return nil
	case "-v", "--version", "version":
		fmt.Fprintln(stdout, version)
		return nil
	default:
		// Headline shorthand: `evaldiff <baseDir> <headDir>` (no
		// subcommand) is the same as `evaldiff risk ...`. Only fires
		// when there are exactly two non-subcommand args, so a typo
		// like `evaldiff indxe foo` still surfaces as "unknown command".
		if len(args) == 2 {
			return runRisk(args, stdout)
		}
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

func runCoverage(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("coverage", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	noTouches := fs.Bool("no-touches", false, "skip the import-based behavior-ref mapping (faster, less useful)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) != 1 {
		return errors.New("usage: evaldiff coverage [--no-touches] <dir>")
	}
	dir := rest[0]
	ctx := context.Background()
	cov, err := coverage.Build(ctx, vfs.OS{}, dir)
	if err != nil {
		return err
	}
	if !*noTouches {
		idx, err := index.Build(ctx, vfs.OS{}, dir)
		if err != nil {
			return fmt.Errorf("build behavior index: %w", err)
		}
		if err := coverage.AttachTouches(ctx, vfs.OS{}, cov, idx); err != nil {
			return fmt.Errorf("attach touches: %w", err)
		}
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(cov)
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

// runRisk is the headline pipeline: build behavior indexes for both
// trees, build coverage for head, attach Touches, diff, intersect, rank,
// and emit the result. Failures at any stage halt with a wrapped error.
//
// --format json (default) emits indented JSON; --format markdown emits a
// PR-comment-shaped markdown blob suitable for the GitHub Action wrapper.
func runRisk(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("risk", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	format := fs.String("format", "json", "output format: json | markdown")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) != 2 {
		return errors.New("usage: evaldiff [risk] [--format json|markdown] <baseDir> <headDir>")
	}
	baseDir, headDir := rest[0], rest[1]
	ctx := context.Background()
	headFs := vfs.OS{}

	baseIdx, err := index.Build(ctx, vfs.OS{}, baseDir)
	if err != nil {
		return fmt.Errorf("build base index: %w", err)
	}
	headIdx, err := index.Build(ctx, headFs, headDir)
	if err != nil {
		return fmt.Errorf("build head index: %w", err)
	}
	cov, err := coverage.Build(ctx, headFs, headDir)
	if err != nil {
		return fmt.Errorf("build head coverage: %w", err)
	}
	if err := coverage.AttachTouches(ctx, headFs, cov, headIdx); err != nil {
		return fmt.Errorf("attach touches: %w", err)
	}
	risk := rank.Compute(diff.Diff(baseIdx, headIdx), headIdx, cov)

	switch *format {
	case "json":
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(risk)
	case "markdown":
		_, err := io.WriteString(stdout, evaldiffio.RenderMarkdown(risk))
		return err
	default:
		return fmt.Errorf("unknown --format %q (want json or markdown)", *format)
	}
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, `usage: evaldiff <baseDir> <headDir>           — print evals at risk (headline)
       evaldiff <subcommand> [args]

subcommands:
  risk [--format json|markdown] <baseDir> <headDir>
                         Same as the headline shorthand. Builds behavior +
                         coverage indexes for head, diffs against base,
                         intersects, ranks, and prints EvalsAtRisk.
                         --format markdown emits a PR-comment blob.
  index [--hash] <dir>   Build behavior index for a source tree (JSON to stdout).
                         --hash prints only the content-addressable hash.
  coverage [--no-touches] <dir>
                         Build the eval-coverage index (test catalog) for a
                         source tree (JSON to stdout). By default each test's
                         Touches list is populated from imports of the same
                         tree's behavior index. --no-touches skips that step.
  diff  <baseDir> <headDir>
                         Diff two source trees, print structured changeset (JSON to stdout).
  version                Print version and exit.
  help                   Show this message.`)
}
