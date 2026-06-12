package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"metriclens/backend/internal/api"
	"metriclens/backend/internal/discovery"
	"metriclens/backend/internal/prober"
	"metriclens/backend/internal/scraper"
	"metriclens/backend/internal/storage"
)

const (
	addr              = ":9999"
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 10 * time.Second
	writeTimeout      = 30 * time.Second
	idleTimeout       = 2 * time.Minute
)

func main() {
	containers, err := discovery.NewDockerDiscovery()
	if err != nil {
		log.Fatal(err)
	}
	interval, err := durationFromEnv(scrapeIntervalEnv, scraper.DefaultInterval)
	if err != nil {
		log.Fatal(err)
	}
	retention, err := durationFromEnv(retentionEnv, storage.DefaultRetention)
	if err != nil {
		log.Fatal(err)
	}
	seriesStore := storage.New(retention)
	scraperService := scraper.New(containers, prober.NewDefault(), nil, seriesStore, interval)
	scraperService.Start(context.Background())

	server := api.NewServer(containers, scraperService, api.Config{
		ScrapeInterval: interval,
		Retention:      retention,
	})

	httpServer := &http.Server{
		Addr:              addr,
		Handler:           server,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}

	log.Printf("metriclens listening on %s", addr)
	if err := httpServer.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
