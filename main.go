// Comando stress-test: teste de carga HTTP via linha de comando.
//
//	stress-test --url=http://google.com --requests=1000 --concurrency=10
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/itallominatti/goexpert-stress-test/internal/loadtest"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("stress-test", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var cfg loadtest.Config
	fs.StringVar(&cfg.URL, "url", "", "URL do serviço a ser testado (obrigatório)")
	fs.IntVar(&cfg.Requests, "requests", 0, "número total de requisições (obrigatório)")
	fs.IntVar(&cfg.Concurrency, "concurrency", 1, "número de chamadas simultâneas")
	fs.DurationVar(&cfg.Timeout, "timeout", 30*time.Second, "tempo máximo de cada requisição")
	fs.StringVar(&cfg.Method, "method", "GET", "método HTTP")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Uso: stress-test --url=<url> --requests=<n> --concurrency=<n> [--timeout=30s] [--method=GET]")
		fmt.Fprintln(stderr)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "erro: argumento inesperado %q\n", fs.Arg(0))
		fs.Usage()
		return 2
	}
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(stderr, "erro: %v\n\n", err)
		fs.Usage()
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Fprintf(stdout, "Iniciando teste: %d requests para %s com concorrência %d...\n\n",
		cfg.Requests, cfg.URL, cfg.Concurrency)

	report, err := loadtest.Run(ctx, cfg)
	if err != nil {
		fmt.Fprintf(stderr, "erro: %v\n", err)
		return 1
	}
	report.Print(stdout)
	if report.Interrupted {
		return 130
	}
	return 0
}
