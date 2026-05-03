// Command evaldiff statically answers "which evals will this PR break?"
// Given a base and head commit, evaldiff diffs the behavior index (prompts,
// tool schemas, agent edges, model IDs, sampling params), intersects with the
// eval-coverage index, and prints the ranked list of affected evals.
package main

import (
	"flag"
	"fmt"
	"os"
)

var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "evaldiff:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("evaldiff", flag.ContinueOnError)
	showVersion := fs.Bool("version", false, "print version and exit")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *showVersion {
		fmt.Println(version)
		return nil
	}

	rest := fs.Args()
	if len(rest) != 2 {
		fs.Usage()
		return fmt.Errorf("usage: evaldiff <base> <head>")
	}
	base, head := rest[0], rest[1]
	fmt.Printf("evaldiff: not yet implemented (base=%s head=%s)\n", base, head)
	return nil
}
