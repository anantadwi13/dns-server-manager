package internal

import "path/filepath"

type Config interface {
	BindFolderPath() string
	NamedConfPath() string

	DataFolderPath() string
	DBName() string
	DBPath() string
}

type config struct {
	bindFolderPath string
	dataFolderPath string
	dbName         string
}

func NewConfig(bindFolderPath string, dataFolderPath string, dbName string) Config {
	conf := &config{
		bindFolderPath: path(bindFolderPath),
		dataFolderPath: path(dataFolderPath),
		dbName:         dbName,
	}
	return conf
}

func (c *config) BindFolderPath() string {
	return c.bindFolderPath
}

func (c *config) NamedConfPath() string {
	return path(c.bindFolderPath, "named.conf")
}

func (c *config) DataFolderPath() string {
	return c.dataFolderPath
}

func (c *config) DBName() string {
	return c.dbName
}

func (c *config) DBPath() string {
	return path(c.dataFolderPath, c.dbName)
}

func path(paths ...string) string {
	cleanPath := ""
	if len(paths) > 0 {
		cleanPath = filepath.Join(paths...)
	}
	return filepath.Clean(cleanPath)
}
