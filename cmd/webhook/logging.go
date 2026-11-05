package main

import (
	"context"
	"log/slog"

	"github.com/go-logr/logr"
)

// logrHandler is a slog.Handler that writes to a logr.Logger, so the
// webhook's own log lines go through cert-manager's logger (klog) and use
// its format and -v flag.
//
// logr.ToSlogHandler is not used because, with klog behind it, it drops
// the attributes added with slog.Logger.With.
type logrHandler struct {
	log    logr.Logger
	prefix string // group prefix for attribute keys, "" or "group."
}

func newSlogLogger(l logr.Logger) *slog.Logger { return slog.New(&logrHandler{log: l}) }

// verbosity maps slog levels to logr V levels: Info is V(0), Debug is V(4).
func verbosity(l slog.Level) int {
	if l >= slog.LevelInfo {
		return 0
	}
	return int(slog.LevelInfo - l)
}

func (h *logrHandler) Enabled(_ context.Context, l slog.Level) bool {
	return l >= slog.LevelError || h.log.V(verbosity(l)).Enabled()
}

func (h *logrHandler) Handle(_ context.Context, r slog.Record) error {
	kv := make([]any, 0, 2*r.NumAttrs())
	var err error
	r.Attrs(func(a slog.Attr) bool {
		if e, ok := a.Value.Any().(error); ok && a.Key == "error" && err == nil && r.Level >= slog.LevelError {
			err = e
			return true
		}
		kv = h.appendAttr(kv, h.prefix, a)
		return true
	})
	if r.Level >= slog.LevelError {
		h.log.Error(err, r.Message, kv...)
		return nil
	}
	h.log.V(verbosity(r.Level)).Info(r.Message, kv...)
	return nil
}

func (h *logrHandler) appendAttr(kv []any, prefix string, a slog.Attr) []any {
	v := a.Value.Resolve()
	if v.Kind() == slog.KindGroup {
		p := prefix
		if a.Key != "" {
			p += a.Key + "."
		}
		for _, g := range v.Group() {
			kv = h.appendAttr(kv, p, g)
		}
		return kv
	}
	if a.Key == "" {
		return kv
	}
	return append(kv, prefix+a.Key, v.Any())
}

func (h *logrHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	var kv []any
	for _, a := range attrs {
		kv = h.appendAttr(kv, h.prefix, a)
	}
	return &logrHandler{log: h.log.WithValues(kv...), prefix: h.prefix}
}

func (h *logrHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &logrHandler{log: h.log, prefix: h.prefix + name + "."}
}
