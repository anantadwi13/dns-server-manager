package main

import (
	"github.com/anantadwi13/dns-server-manager/internal"
)

const (
	BindFolderPath = "/etc/bind/"

	DataPath = "/data/"
	DBName   = "service.sqlite.db"
)

func main() {
	service := internal.NewService(
		internal.NewConfig(BindFolderPath, DataPath, DBName),
	)
	service.Start()
}
