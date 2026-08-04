package gateway

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type Server interface {
	Start() error
	Shutdown(context.Context) error
}

type Runtime struct {
	server            Server
	shutdownTimeout   time.Duration
	pprofAddr         string
	logger            *slog.Logger
	serverClosedError error
	startHooks        []hook
	jobs              []job
	shutdownHooks     []shutdownHook
}

type Options struct {
	ShutdownTimeout   time.Duration
	PprofAddr         string
	Logger            *slog.Logger
	ServerClosedError error
}

type hook struct {
	name string
	fn   func(context.Context)
}

type job struct {
	name string
	fn   func(context.Context)
}

type shutdownHook struct {
	name string
	fn   func(context.Context) error
}

func NewRuntime(server Server, opts Options) *Runtime {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	if opts.ShutdownTimeout <= 0 {
		opts.ShutdownTimeout = 10 * time.Second
	}
	return &Runtime{
		server:            server,
		shutdownTimeout:   opts.ShutdownTimeout,
		pprofAddr:         opts.PprofAddr,
		logger:            logger,
		serverClosedError: opts.ServerClosedError,
	}
}

func (r *Runtime) OnStart(name string, fn func(context.Context)) {
	if fn != nil {
		r.startHooks = append(r.startHooks, hook{name: name, fn: fn})
	}
}

func (r *Runtime) Go(name string, fn func(context.Context)) {
	if fn != nil {
		r.jobs = append(r.jobs, job{name: name, fn: fn})
	}
}

func (r *Runtime) OnShutdown(name string, fn func(context.Context) error) {
	if fn != nil {
		r.shutdownHooks = append(r.shutdownHooks, shutdownHook{name: name, fn: fn})
	}
}

func (r *Runtime) Run(parent context.Context) error {
	if r.server == nil {
		return errors.New("gateway runtime: server is required")
	}
	runCtx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()

	for _, item := range r.startHooks {
		item.fn(runCtx)
	}
	if r.pprofAddr != "" {
		go r.runPprof()
	}
	for _, item := range r.jobs {
		jobItem := item
		go func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					r.logger.Error("gateway runtime job panic", "job", jobItem.name, "recover", recovered)
				}
			}()
			jobItem.fn(runCtx)
		}()
	}

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- r.server.Start()
	}()

	select {
	case <-runCtx.Done():
	case err := <-serverErr:
		if !r.isServerClosed(err) {
			r.logger.Error("server stopped with error", "error", err)
			stop()
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), r.shutdownTimeout)
	defer cancel()

	var errs []error
	if err := r.server.Shutdown(shutdownCtx); err != nil {
		r.logger.Error("shutdown error", "error", err)
		errs = append(errs, err)
	}
	for _, item := range r.shutdownHooks {
		if err := item.fn(shutdownCtx); err != nil {
			r.logger.Warn("gateway runtime shutdown hook failed", "hook", item.name, "error", err)
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (r *Runtime) runPprof() {
	r.logger.Info("pprof listening", "addr", r.pprofAddr)
	if err := http.ListenAndServe(r.pprofAddr, nil); err != nil {
		r.logger.Error("pprof server stopped", "error", err)
	}
}

func (r *Runtime) isServerClosed(err error) bool {
	if err == nil {
		return true
	}
	return r.serverClosedError != nil && errors.Is(err, r.serverClosedError)
}
