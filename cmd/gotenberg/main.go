package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/thecodingmachine/gotenberg/internal/app/api"
	"github.com/thecodingmachine/gotenberg/internal/pkg/notify"
	"github.com/thecodingmachine/gotenberg/internal/pkg/pm2"
)

// version will be set on build time.
// nolint: gochecknoglobals
var version = "snapshot"

const (
	defaultWaitTimeoutEnvVar  = "DEFAULT_WAIT_TIMEOUT"
	disableGoogleChromeEnvVar = "DISABLE_GOOGLE_CHROME"
	disableUnoconvEnvVar      = "DISABLE_UNOCONV"
)

func mustParseEnvVar() *api.Options {
	opts := api.DefaultOptions()
	if os.Getenv(defaultWaitTimeoutEnvVar) != "" {
		defaultWaitTimeout, err := strconv.ParseFloat(os.Getenv(defaultWaitTimeoutEnvVar), 64)
		if err != nil {
			notify.ErrPrint(fmt.Errorf("%s: wrong value: want float got %v", defaultWaitTimeoutEnvVar, err))
			os.Exit(1)
		}
		opts.DefaultWaitTimeout = defaultWaitTimeout
	}
	if v, ok := os.LookupEnv(disableGoogleChromeEnvVar); ok {
		if v != "1" && v != "0" {
			notify.ErrPrint(fmt.Errorf("%s: wrong value: want \"0\" or \"1\" got %v", defaultWaitTimeoutEnvVar, v))
			os.Exit(1)
		}
		opts.EnableChromeEndpoints = v != "1"
	}
	if v, ok := os.LookupEnv(disableUnoconvEnvVar); ok {
		if v != "1" && v != "0" {
			notify.ErrPrint(fmt.Errorf("%s: wrong value: want \"0\" or \"1\" got %v", disableUnoconvEnvVar, v))
			os.Exit(1)
		}
		opts.EnableUnoconvEndpoints = v != "1"
	}
	return opts
}

func mustStartProcesses(opts *api.Options) []pm2.Process {
	var processes []pm2.Process
	if opts.EnableChromeEndpoints {
		processes = append(processes, pm2.NewChrome())
	}
	if opts.EnableUnoconvEndpoints {
		processes = append(processes, pm2.NewUnoconv())
	}
	for _, p := range processes {
		notify.Printf("starting %s with PM2...", p.Fullname())
		if err := p.Start(); err != nil {
			notify.ErrPrint(err)
			os.Exit(1)
		}
	}
	return processes
}

func mustStartAPI(srv *echo.Echo) {
	notify.Print("http server started on port 3000")
	if err := srv.Start(":3000"); err != nil {
		if err != http.ErrServerClosed {
			notify.ErrPrint(err)
			os.Exit(1)
		}
	}
}

func mustShutdownProcesses(processes []pm2.Process) {
	for _, p := range processes {
		notify.Printf("shutting down %s with PM2... (Ctrl+C to force)", p.Fullname())
		if err := p.Shutdown(); err != nil {
			notify.ErrPrint(err)
			os.Exit(1)
		}
	}
}

func mustShutdownAPI(srv *echo.Echo) {
	// create a deadline to wait for.
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	// doesn't block if no connections, but will otherwise wait
	// until the timeout deadline.
	notify.Print("shutting down http server... (Ctrl+C to force)")
	if err := srv.Shutdown(ctx); err != nil {
		notify.ErrPrint(err)
		os.Exit(1)
	}
}

func main() {
	notify.Printf("Gotenberg %s", version)
	opts := mustParseEnvVar()
	srv := api.New(opts)
	processes := mustStartProcesses(opts)
	// run our API in a goroutine so that it doesn't block.s
	go func() {
		mustStartAPI(srv)
	}()
	quit := make(chan os.Signal, 1)
	// we'll accept graceful shutdowns when quit via SIGINT (Ctrl+C)
	// SIGKILL, SIGQUIT or SIGTERM (Ctrl+/) will not be caught.
	signal.Notify(quit, os.Interrupt)
	// block until we receive our signal.
	<-quit
	mustShutdownAPI(srv)
	mustShutdownProcesses(processes)
	notify.Print("bye!")
	os.Exit(0)
}
