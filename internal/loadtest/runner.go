// Package loadtest executa testes de carga HTTP e consolida o resultado.
package loadtest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

const maxRedirects = 10

// Config descreve um teste de carga.
type Config struct {
	URL         string
	Requests    int
	Concurrency int
	Timeout     time.Duration
	Method      string
}

// Validate confere os parâmetros antes de começar.
func (c *Config) Validate() error {
	if strings.TrimSpace(c.URL) == "" {
		return errors.New("--url é obrigatório")
	}
	u, err := url.Parse(c.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("--url inválida: %q (use http:// ou https://)", c.URL)
	}
	if c.Requests <= 0 {
		return errors.New("--requests deve ser maior que zero")
	}
	if c.Concurrency <= 0 {
		return errors.New("--concurrency deve ser maior que zero")
	}
	if c.Timeout <= 0 {
		return errors.New("--timeout deve ser maior que zero")
	}
	if c.Method == "" {
		c.Method = http.MethodGet
	}
	return nil
}

// Result é o resultado de uma requisição individual.
type Result struct {
	Status   int // 0 quando não houve resposta HTTP
	Err      error
	Duration time.Duration
}

// Run dispara exatamente cfg.Requests requisições usando cfg.Concurrency
// workers e devolve o relatório consolidado. Se ctx for cancelado, para de
// enviar novas requisições e o relatório traz apenas o que foi feito.
func Run(ctx context.Context, cfg Config) (*Report, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	workers := cfg.Concurrency
	if workers > cfg.Requests {
		workers = cfg.Requests
	}

	client := &http.Client{
		Timeout: cfg.Timeout,
		// Segue redirecionamentos (ex.: google.com -> www.google.com), mas num
		// laço de redirecionamento conta o último código recebido em vez de
		// transformar a requisição em erro sem status.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return http.ErrUseLastResponse
			}
			return nil
		},
		Transport: &http.Transport{
			Proxy:               http.ProxyFromEnvironment,
			MaxIdleConns:        workers,
			MaxIdleConnsPerHost: workers,
			MaxConnsPerHost:     workers,
			IdleConnTimeout:     90 * time.Second,
			ForceAttemptHTTP2:   true,
		},
	}
	defer client.CloseIdleConnections()

	// O canal de trabalhos recebe exatamente cfg.Requests itens: cada item
	// consumido vira uma requisição, então o total é sempre o pedido.
	jobs := make(chan struct{}, cfg.Requests)
	for i := 0; i < cfg.Requests; i++ {
		jobs <- struct{}{}
	}
	close(jobs)

	results := make(chan Result, workers*2)
	var wg sync.WaitGroup

	start := time.Now()
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range jobs {
				if ctx.Err() != nil {
					return
				}
				results <- doRequest(ctx, client, cfg)
			}
		}()
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	rep := newReport(cfg, workers)
	for r := range results {
		rep.add(r)
	}
	rep.TotalDuration = time.Since(start)
	rep.Interrupted = ctx.Err() != nil && rep.Total < cfg.Requests
	rep.finish()
	return rep, nil
}

func doRequest(ctx context.Context, client *http.Client, cfg Config) Result {
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, cfg.Method, cfg.URL, nil)
	if err != nil {
		return Result{Err: err, Duration: time.Since(start)}
	}
	req.Header.Set("User-Agent", "goexpert-stress-test/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return Result{Err: err, Duration: time.Since(start)}
	}
	// Ler o corpo inteiro permite reaproveitar a conexão (keep-alive).
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return Result{Status: resp.StatusCode, Duration: time.Since(start)}
}

// classifyError agrupa erros de rede em categorias legíveis.
func classifyError(err error) string {
	var netErr net.Error
	var dnsErr *net.DNSError
	var opErr *net.OpError
	switch {
	case errors.Is(err, context.Canceled):
		return "cancelada"
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &netErr) && netErr.Timeout():
		return "timeout"
	case errors.As(err, &dnsErr):
		return "falha de DNS"
	case strings.Contains(err.Error(), "connection refused"):
		return "conexão recusada"
	case strings.Contains(err.Error(), "connection reset"):
		return "conexão reiniciada"
	case strings.Contains(err.Error(), "tls:") || strings.Contains(err.Error(), "x509:"):
		return "erro de TLS"
	case errors.As(err, &opErr):
		return "erro de rede: " + shortMsg(opErr.Err)
	default:
		return shortMsg(err)
	}
}

// shortMsg tira o prefixo `Get "url":` e limita o tamanho, para que erros
// desconhecidos ainda apareçam agrupados e legíveis no relatório.
func shortMsg(err error) string {
	var uErr *url.Error
	if errors.As(err, &uErr) {
		err = uErr.Err
	}
	msg := err.Error()
	if len(msg) > 60 {
		msg = msg[:57] + "..."
	}
	return msg
}

func sortedKeys[K int | string, V any](m map[K]V) []K {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}
