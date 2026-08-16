// Command ejina-microgrid is the dispatch service for the Ejina Banner
// microgrid, coordinating grid-forming energy storage battery cabins,
// wind/solar arrays, and the microgrid master controller under normal
// operation, off-grid black start, and daily inspection.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"ejina-microgrid/internal/app"
	"ejina-microgrid/internal/config"
	"ejina-microgrid/internal/job"
	"ejina-microgrid/internal/server"
	"ejina-microgrid/internal/store"
)

func main() {
	cfgPath := os.Getenv("EJINA_CONFIG")
	if cfgPath == "" {
		cfgPath = "config.json"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	st := store.New(cfg.StorePath)
	application := app.New(cfg, st)

	if err := application.LoadSnapshot(); err != nil {
		log.Printf("load persisted snapshot: %v (continuing with fresh state)", err)
	}

	scheduler := job.New(application).WithDefaults()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	scheduler.Start(ctx)

	srv := server.New(application)
	httpServer := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("ejina-microgrid dispatch service listening on %s", cfg.ListenAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http server: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("http shutdown: %v", err)
	}
	if err := application.SaveSnapshot(); err != nil {
		log.Printf("final snapshot save: %v", err)
	}
	scheduler.Wait()
	log.Println("shutdown complete")
}
