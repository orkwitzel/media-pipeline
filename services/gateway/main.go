package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func mustRetry(name string, fn func() error) {
	for i := 0; i < 30; i++ {
		if err := fn(); err == nil { return } else { log.Printf("waiting for %s: %v", name, err) }
		time.Sleep(2 * time.Second)
	}
	log.Fatalf("%s never became ready", name)
}

func main() {
	cfg := LoadConfig()
	ctx := context.Background()
	var (store *PgStore; obj *Minio; broker *RabbitBroker; cache *RedisCache; err error)
	mustRetry("postgres", func() error { store, err = NewStore(ctx, cfg.DatabaseURL); return err })
	mustRetry("minio", func() error { obj, err = NewMinio(cfg); if err != nil { return err }; return obj.EnsureBuckets(ctx) })
	mustRetry("rabbitmq", func() error { broker, err = NewBroker(cfg.RabbitURL); return err })
	mustRetry("redis", func() error { cache, err = NewCache(cfg.RedisURL); return err })

	app := &App{Obj: obj, Store: store, Broker: broker, Cache: cache, Cfg: cfg}
	srv := &http.Server{Addr: ":" + cfg.Port, Handler: app.Router()}

	go func() {
		log.Printf("gateway listening on :%s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) { log.Fatal(err) }
	}()
	stop := make(chan os.Signal, 1); signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
	<-stop
	log.Println("shutting down")
	sctx, cancel := context.WithTimeout(context.Background(), 25*time.Second); defer cancel()
	_ = srv.Shutdown(sctx)
}
