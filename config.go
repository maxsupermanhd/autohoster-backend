package main

import (
	"autohoster-backend/ispcheck"
	"autohoster-backend/mapstorage"
	"log"

	"github.com/maxsupermanhd/lac/v2"
)

var (
	cfg lac.Conf

	ms         *mapstorage.Mapstorage
	ISPchecker *ispcheck.ISPChecker
)

func loadConfig() {
	var err error
	cfg, err = lac.FromFileJSON("config.json")
	if err != nil {
		log.Fatalf("Failed to read config: %s", err.Error())
	}
	ms, err = mapstorage.NewMapstorage(cfg.LinkSubTree("mapstorage"))
	if err != nil {
		log.Fatalf("Failed to init map storage: %s", err.Error())
	}
	ISPchecker = ispcheck.NewISPChecker(cfg.LinkSubTree("ispcheck"))
}
