package loadtest

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Report consolida o resultado de um teste de carga.
type Report struct {
	URL         string
	Requested   int
	Concurrency int

	TotalDuration time.Duration
	Total         int            // requisições realizadas (com ou sem resposta)
	Status200     int            // respostas com HTTP 200
	StatusCounts  map[int]int    // todos os códigos HTTP recebidos
	Errors        map[string]int // falhas sem resposta HTTP, por categoria
	Interrupted   bool

	latencies []time.Duration
	Min       time.Duration
	Max       time.Duration
	Avg       time.Duration
	P50       time.Duration
	P90       time.Duration
	P99       time.Duration
}

func newReport(cfg Config, workers int) *Report {
	return &Report{
		URL:          cfg.URL,
		Requested:    cfg.Requests,
		Concurrency:  workers,
		StatusCounts: map[int]int{},
		Errors:       map[string]int{},
		latencies:    make([]time.Duration, 0, cfg.Requests),
	}
}

func (r *Report) add(res Result) {
	r.Total++
	r.latencies = append(r.latencies, res.Duration)
	if res.Err != nil {
		r.Errors[classifyError(res.Err)]++
		return
	}
	r.StatusCounts[res.Status]++
	if res.Status == http.StatusOK {
		r.Status200++
	}
}

func (r *Report) finish() {
	if len(r.latencies) == 0 {
		return
	}
	sort.Slice(r.latencies, func(i, j int) bool { return r.latencies[i] < r.latencies[j] })
	var sum time.Duration
	for _, d := range r.latencies {
		sum += d
	}
	r.Min = r.latencies[0]
	r.Max = r.latencies[len(r.latencies)-1]
	r.Avg = sum / time.Duration(len(r.latencies))
	r.P50 = percentile(r.latencies, 50)
	r.P90 = percentile(r.latencies, 90)
	r.P99 = percentile(r.latencies, 99)
}

// percentile usa o método nearest-rank sobre uma lista já ordenada.
func percentile(sorted []time.Duration, p int) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	rank := (p*len(sorted) + 99) / 100 // ceil(p/100 * n)
	if rank < 1 {
		rank = 1
	}
	return sorted[rank-1]
}

// ErrorCount soma todas as falhas sem resposta HTTP.
func (r *Report) ErrorCount() int {
	n := 0
	for _, c := range r.Errors {
		n += c
	}
	return n
}

// RequestsPerSecond é a vazão média do teste.
func (r *Report) RequestsPerSecond() float64 {
	if r.TotalDuration <= 0 {
		return 0
	}
	return float64(r.Total) / r.TotalDuration.Seconds()
}

// Print escreve o relatório legível no writer.
func (r *Report) Print(w io.Writer) {
	line := strings.Repeat("=", 52)
	fmt.Fprintln(w, line)
	fmt.Fprintln(w, " RELATÓRIO DO TESTE DE CARGA")
	fmt.Fprintln(w, line)
	fmt.Fprintf(w, " URL:                     %s\n", r.URL)
	fmt.Fprintf(w, " Concorrência:            %d\n", r.Concurrency)
	fmt.Fprintf(w, " Tempo total:             %s\n", round(r.TotalDuration))
	fmt.Fprintf(w, " Requests solicitados:    %d\n", r.Requested)
	fmt.Fprintf(w, " Requests realizados:     %d\n", r.Total)
	fmt.Fprintf(w, " Requests com HTTP 200:   %d\n", r.Status200)
	fmt.Fprintf(w, " Vazão média:             %.2f req/s\n", r.RequestsPerSecond())
	if r.Interrupted {
		fmt.Fprintln(w, " ATENÇÃO: teste interrompido antes de terminar.")
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, " Distribuição dos códigos HTTP:")
	if len(r.StatusCounts) == 0 {
		fmt.Fprintln(w, "   (nenhuma resposta HTTP recebida)")
	}
	for _, code := range sortedKeys(r.StatusCounts) {
		count := r.StatusCounts[code]
		fmt.Fprintf(w, "   %d %-28s %8d  (%5.1f%%)\n", code, statusText(code), count, pct(count, r.Total))
	}

	if len(r.Errors) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintf(w, " Falhas sem resposta HTTP: %d\n", r.ErrorCount())
		for _, kind := range sortedKeys(r.Errors) {
			count := r.Errors[kind]
			fmt.Fprintf(w, "   %-32s %8d  (%5.1f%%)\n", kind, count, pct(count, r.Total))
		}
	}

	if r.Total > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, " Latência por request:")
		fmt.Fprintf(w, "   mín %s | média %s | p50 %s | p90 %s | p99 %s | máx %s\n",
			round(r.Min), round(r.Avg), round(r.P50), round(r.P90), round(r.P99), round(r.Max))
	}
	fmt.Fprintln(w, line)
}

func statusText(code int) string {
	if t := http.StatusText(code); t != "" {
		return t
	}
	return "Desconhecido"
}

func pct(part, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) * 100 / float64(total)
}

func round(d time.Duration) time.Duration {
	switch {
	case d >= time.Second:
		return d.Round(time.Millisecond)
	case d >= time.Millisecond:
		return d.Round(10 * time.Microsecond)
	default:
		return d.Round(time.Microsecond)
	}
}
