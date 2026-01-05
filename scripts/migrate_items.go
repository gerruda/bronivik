package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"bronivik/internal/database"
	"bronivik/internal/models"
	"gopkg.in/yaml.v3"
)

type ItemsConfig struct {
	Items []models.Item `yaml:"items"`
}

func main() {
	var (
		itemsPath = flag.String("items", "configs/items.yaml", "path to items.yaml")
		dbPath    = flag.String("db", "./data/bookings.db", "path to sqlite db")
	)
	flag.Parse()

	data, err := os.ReadFile(*itemsPath)
	if err != nil {
		log.Fatalf("read items: %v", err)
	}
	var cfg ItemsConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		log.Fatalf("parse items: %v", err)
	}
	if len(cfg.Items) == 0 {
		log.Fatal("no items in yaml")
	}

	db, err := database.NewDB(*dbPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	created := 0
	updated := 0
	for _, it := range cfg.Items {
		if it.Name == "" {
			continue
		}
		_, err := db.GetItemByName(ctx, it.Name)
		if err == nil {
			if err := db.UpdateItem(ctx, &it); err != nil {
				log.Fatalf("update %s: %v", it.Name, err)
			}
			updated++
			continue
		}
		if err != nil && err != sql.ErrNoRows {
			log.Fatalf("get %s: %v", it.Name, err)
		}
		if err := db.CreateItem(ctx, &it); err != nil {
			log.Fatalf("create %s: %v", it.Name, err)
		}
		created++
	}

	fmt.Printf("done: created=%d updated=%d\n", created, updated)
}
