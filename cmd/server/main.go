// Command server is the runnable entry point for the oyster purification
// release gate service. It opens the SQLite store, seeds the rule catalog,
// assembles the HTTP API and serves it, running a restart-recovery scan on
// startup.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"oyster-purification-release-gate/adjudication"
	"oyster-purification-release-gate/catalog"
	"oyster-purification-release-gate/domain"
	"oyster-purification-release-gate/httpapi"
	"oyster-purification-release-gate/service"
	"oyster-purification-release-gate/store"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	dbPath := flag.String("db", "release.db", "sqlite database path (use :memory: for ephemeral)")
	printCatalog := flag.Bool("print-catalog", false, "print the seeded rule catalog as JSON and exit")
	flag.Parse()

	cat := catalog.DefaultCatalog()
	if *printCatalog {
		printCatalogJSON(cat)
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, *dbPath, domain.FixedClock(0))
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	// Recover any device call left mid-flight by a previous crash.
	svc := service.New(st, cat, adjudication.StaticAdapter{Payload: []byte(`{"norovirus_ct":"18.0","coliform":"2"}`)})
	if n, err := svc.Recover(ctx, 1<<62); err != nil {
		log.Printf("recovery scan: %v", err)
	} else if n > 0 {
		log.Printf("recovered %d pending device calls", n)
	}

	handler := httpapi.NewHandler(svc, dbHealth{store: st})

	srv := &http.Server{Addr: *addr, Handler: handler}
	go func() {
		log.Printf("listening on %s", *addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server exited: %v", err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	log.Println("server stopped")
}

func printCatalogJSON(cat *catalog.Catalog) {
	snap, ok := cat.Get(catalog.DefaultSnapshotID)
	if !ok {
		log.Fatal("default snapshot missing")
	}
	out := map[string]any{
		"id":     snap.ID,
		"area":   snap.AreaID,
		"digest": snap.Digest,
		"tides":  snap.Tides,
	}
	enc := json.NewEncoder(os.Stdout)
	_ = enc.Encode(out)
}

// dbHealth reports readiness based on database reachability.
type dbHealth struct {
	store *store.Store
}

func (h dbHealth) Healthy() bool {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	return h.store.Ping(ctx) == nil
}
