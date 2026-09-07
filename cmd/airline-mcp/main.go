package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"r-project1/internal/airline/mcpserver"
	"r-project1/internal/airline/store"
)

type options struct {
	transport   string
	addr        string
	databaseURL string
}

func parseFlags() (options, error) {
	var o options
	flag.StringVar(&o.transport, "transport", "stdio", "transport to serve on, either stdio or http")
	flag.StringVar(&o.addr, "addr", ":8080", "listen address, used only when transport is http")
	flag.Parse()

	o.databaseURL = os.Getenv("DATABASE_URL")

	if o.transport != "stdio" && o.transport != "http" {
		return o, fmt.Errorf("unknown transport %q, expected stdio or http", o.transport)
	}
	return o, nil
}

func main() {
	o, err := parseFlags()
	if err != nil {
		fmt.Fprintln(os.Stderr, "airline-mcp:", err)
		os.Exit(2)
	}
	if err := run(o); err != nil {
		fmt.Fprintln(os.Stderr, "airline-mcp:", err)
		os.Exit(1)
	}
}

func run(o options) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.New(ctx, o.databaseURL)
	if err != nil {
		return err
	}
	defer st.Close()

	srv := mcpserver.New(st)

	if o.transport == "http" {
		return serveHTTP(ctx, srv, st, o.addr)
	}

	// In stdio mode stdout carries the protocol, so diagnostics go to stderr.
	fmt.Fprintf(os.Stderr, "airline-mcp %s serving on stdio\n", mcpserver.Version)
	return srv.Run(ctx, &mcp.StdioTransport{})
}

func serveHTTP(ctx context.Context, srv *mcp.Server, st *store.Store, addr string) error {
	httpSrv := &http.Server{
		Addr:    addr,
		Handler: mcpserver.NewHTTPHandler(srv, st.Ping),
		// No WriteTimeout: streamable HTTP holds responses open for SSE.
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutdownCtx)
	}()

	fmt.Fprintf(os.Stderr, "airline-mcp %s serving on http %s, mcp at %s, health at /healthz\n",
		mcpserver.Version, addr, mcpserver.MCPPath)

	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
