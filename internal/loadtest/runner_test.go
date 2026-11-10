package loadtest

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func cfgFor(url string, requests, concurrency int) Config {
	return Config{URL: url, Requests: requests, Concurrency: concurrency, Timeout: 5 * time.Second}
}

func TestRunExactRequestCount(t *testing.T) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	for _, tc := range []struct{ requests, concurrency int }{
		{1, 1}, {7, 3}, {100, 10}, {101, 10}, {5, 50},
	} {
		hits.Store(0)
		rep, err := Run(context.Background(), cfgFor(srv.URL, tc.requests, tc.concurrency))
		if err != nil {
			t.Fatal(err)
		}
		if got := int(hits.Load()); got != tc.requests {
			t.Errorf("requests=%d concurrency=%d: servidor recebeu %d", tc.requests, tc.concurrency, got)
		}
		if rep.Total != tc.requests || rep.Status200 != tc.requests {
			t.Errorf("relatório: total=%d status200=%d, esperado %d", rep.Total, rep.Status200, tc.requests)
		}
	}
}

func TestRunRespectsConcurrency(t *testing.T) {
	var inFlight, peak atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := inFlight.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		inFlight.Add(-1)
	}))
	defer srv.Close()

	if _, err := Run(context.Background(), cfgFor(srv.URL, 60, 6)); err != nil {
		t.Fatal(err)
	}
	if p := peak.Load(); p > 6 {
		t.Errorf("pico de %d requisições simultâneas, limite era 6", p)
	}
	if p := peak.Load(); p < 2 {
		t.Errorf("pico de %d: as requisições não rodaram em paralelo", p)
	}
}

func TestRunStatusDistribution(t *testing.T) {
	var n atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch n.Add(1) % 4 {
		case 0:
			w.WriteHeader(http.StatusNotFound)
		case 1:
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	rep, err := Run(context.Background(), cfgFor(srv.URL, 40, 4))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Status200 != 20 || rep.StatusCounts[404] != 10 || rep.StatusCounts[500] != 10 {
		t.Errorf("distribuição inesperada: 200=%d %v", rep.Status200, rep.StatusCounts)
	}

	var out bytes.Buffer
	rep.Print(&out)
	for _, want := range []string{"Requests realizados:     40", "Requests com HTTP 200:   20", "404 Not Found", "500 Internal Server Error"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("relatório não contém %q:\n%s", want, out.String())
		}
	}
}

func TestRunCountsConnectionErrors(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close() // porta livre: conexão recusada

	rep, err := Run(context.Background(), cfgFor("http://"+addr, 5, 2))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Total != 5 || rep.ErrorCount() != 5 || rep.Status200 != 0 {
		t.Errorf("total=%d erros=%d 200=%d", rep.Total, rep.ErrorCount(), rep.Status200)
	}
}

func TestRunTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
	}))
	defer srv.Close()

	cfg := cfgFor(srv.URL, 2, 2)
	cfg.Timeout = 50 * time.Millisecond
	rep, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Errors["timeout"] != 2 {
		t.Errorf("esperava 2 timeouts, veio %v", rep.Errors)
	}
}

func TestRunRedirects(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/ok", func(w http.ResponseWriter, r *http.Request) {})
	mux.HandleFunc("/vai", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/ok", http.StatusMovedPermanently) })
	mux.HandleFunc("/laco", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/laco", http.StatusFound) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	rep, err := Run(context.Background(), cfgFor(srv.URL+"/vai", 3, 1))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Status200 != 3 {
		t.Errorf("redirecionamento simples: esperava 3 x 200, veio %v", rep.StatusCounts)
	}

	rep, err = Run(context.Background(), cfgFor(srv.URL+"/laco", 3, 1))
	if err != nil {
		t.Fatal(err)
	}
	if rep.StatusCounts[302] != 3 || rep.ErrorCount() != 0 {
		t.Errorf("laço: esperava 3 x 302 sem erros, veio %v %v", rep.StatusCounts, rep.Errors)
	}
}

func TestValidate(t *testing.T) {
	bad := []Config{
		{URL: "", Requests: 1, Concurrency: 1, Timeout: time.Second},
		{URL: "google.com", Requests: 1, Concurrency: 1, Timeout: time.Second},
		{URL: "ftp://x", Requests: 1, Concurrency: 1, Timeout: time.Second},
		{URL: "http://x", Requests: 0, Concurrency: 1, Timeout: time.Second},
		{URL: "http://x", Requests: 1, Concurrency: 0, Timeout: time.Second},
		{URL: "http://x", Requests: 1, Concurrency: 1, Timeout: 0},
	}
	for _, c := range bad {
		if err := c.Validate(); err == nil {
			t.Errorf("esperava erro para %+v", c)
		}
	}
	ok := Config{URL: "https://example.com/a?b=1", Requests: 1, Concurrency: 1, Timeout: time.Second}
	if err := ok.Validate(); err != nil {
		t.Errorf("config válida rejeitada: %v", err)
	}
}

func TestPercentile(t *testing.T) {
	var d []time.Duration
	for i := 1; i <= 100; i++ {
		d = append(d, time.Duration(i))
	}
	if percentile(d, 50) != 50 || percentile(d, 90) != 90 || percentile(d, 99) != 99 {
		t.Errorf("p50=%d p90=%d p99=%d", percentile(d, 50), percentile(d, 90), percentile(d, 99))
	}
}
