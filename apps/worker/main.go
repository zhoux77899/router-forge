package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/zhoux77899/router-forge/internal/domain"
	"github.com/zhoux77899/router-forge/internal/executor"
)

func run(args []string, input io.ReadCloser, output io.WriteCloser, stderr io.Writer) int {
	flags := flag.NewFlagSet("router-forge-worker", flag.ContinueOnError)
	flags.SetOutput(stderr)
	stdio := flags.Bool("stdio", false, "serve private HTTP/2 over stdin/stdout")
	version := flags.Bool("version", false, "print worker version")
	if flags.Parse(args) != nil {
		return 2
	}
	if flags.NArg() != 0 || *stdio == *version {
		_, _ = fmt.Fprintln(stderr, "specify exactly one of --stdio or --version")
		return 2
	}
	if *version {
		_, _ = fmt.Fprintln(output, domain.Version)
		return 0
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := executor.RunWorker(ctx, input, output); err != nil {
		_, _ = fmt.Fprintln(stderr, "worker connection closed; owned work cancelled")
		return 1
	}
	return 0
}
func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }
