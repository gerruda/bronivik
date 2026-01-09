package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"time"

	"bronivik/internal/database"
	"bronivik/internal/models"

	"github.com/rs/zerolog"
	"gopkg.in/yaml.v3"
)

type ItemsConfig struct {
	Items []models.Item `yaml:"items"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()
	var (
		itemsPath = flag.String("items", "configs/items.yaml", "path to items.yaml")
		dbPath    = flag.String("db", "./data/bookings.db", "path to sqlite db")
	)
	flag.Parse()

	data, err := os.ReadFile(*itemsPath)
	if err != nil {
		return fmt.Errorf("read items: %w", err)
	}
	var cfg ItemsConfig
	if err = yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse items: %w", err)
	}
	if len(cfg.Items) == 0 {
		return fmt.Errorf("no items in yaml")
	}

	db, err := database.NewDB(*dbPath, &logger)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
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
		_, err = db.GetItemByName(ctx, it.Name)
		if err == nil {
			if err = db.UpdateItem(ctx, &it); err != nil {
				return fmt.Errorf("update %s: %w", it.Name, err)
			}
			updated++
			continue
		}
		if err != sql.ErrNoRows {
			return fmt.Errorf("get %s: %w", it.Name, err)
		}
		if err = db.CreateItem(ctx, &it); err != nil {
			return fmt.Errorf("create %s: %w", it.Name, err)
		}
		created++
	}

	fmt.Printf("done: created=%d updated=%d\n", created, updated)
	return nil
}
