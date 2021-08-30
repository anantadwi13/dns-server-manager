package main

import (
	"github.com/anantadwi13/dns-server-manager/internal"
	"github.com/anantadwi13/dns-server-manager/internal/domain"
)

const (
	BindFolderPath = "/etc/bind/"

	DataPath = "/data/"
	DBName   = "service.sqlite.db"
)

func main() {
	service := internal.NewService(
		domain.NewConfig(BindFolderPath, DataPath, DBName),
	)
	service.Start()
}
