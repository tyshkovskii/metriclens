package main

import (
	"context"
	"log"
	"net/http"

	"metriclens/backend/internal/api"
	"metriclens/backend/internal/discovery"
	"metriclens/backend/internal/prober"
	"metriclens/backend/internal/scraper"
	"metriclens/backend/internal/storage"
)

func main() {
	addr := ":9999"

	containers, err := discovery.NewDockerDiscovery()
	if err != nil {
		log.Fatal(err)
	}
	interval, err := scraper.IntervalFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	retention, err := storage.RetentionFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	seriesStore := storage.New(retention)
	scraperService := scraper.New(containers, prober.NewDefault(), nil, seriesStore, interval)
	scraperService.Start(context.Background())

	server := api.NewServer(containers, scraperService)

	log.Printf("metriclens listening on %s", addr)
	if err := http.ListenAndServe(addr, server); err != nil {
		log.Fatal(err)
	}
}
