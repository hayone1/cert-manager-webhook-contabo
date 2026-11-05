package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"github.com/go-logr/logr/funcr"
)

func TestLogrHandler(t *testing.T) {
	var lines []string
	l := funcr.New(func(prefix, args string) {
		lines = append(lines, prefix+" "+args)
	}, funcr.Options{Verbosity: 1})
	log := newSlogLogger(l.WithName("contabo"))

	log.With("fqdn", "x.example.com").With("zone", "example.com").Info("created", "recordID", 5)
	log.Debug("hidden at verbosity 1")
	log.WithGroup("g").Info("grouped", "k", "v")
	log.Error("failed", "error", errors.New("boom"), "zone", "example.com")

	out := strings.Join(lines, "\n")
	for _, want := range []string{
		`"fqdn"="x.example.com"`, `"zone"="example.com"`, `"recordID"=5`,
		`"g.k"="v"`, `"error"="boom"`, `"msg"="failed"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "hidden") {
		t.Errorf("debug line should be hidden:\n%s", out)
	}
	if verbosity(-4) != 4 || verbosity(0) != 0 || verbosity(8) != 0 {
		t.Error("bad verbosity mapping")
	}
	_ = fmt.Sprint(logr.Discard())
}
