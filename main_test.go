package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunCLI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	var out, errOut bytes.Buffer
	code := run([]string{"--url=" + srv.URL, "--requests=25", "--concurrency=5"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("código %d, stderr: %s", code, errOut.String())
	}
	for _, want := range []string{"Tempo total:", "Requests realizados:     25", "Requests com HTTP 200:   25"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("saída não contém %q:\n%s", want, out.String())
		}
	}
}

func TestRunCLIInvalidArgs(t *testing.T) {
	cases := [][]string{
		{},
		{"--url=http://x", "--requests=0", "--concurrency=1"},
		{"--url=http://x", "--requests=10", "--concurrency=-1"},
		{"--url=nada", "--requests=10", "--concurrency=1"},
		{"--requests=abc"},
	}
	for _, args := range cases {
		var out, errOut bytes.Buffer
		if code := run(args, &out, &errOut); code != 2 {
			t.Errorf("args %v: esperava código 2, veio %d", args, code)
		}
	}
}
