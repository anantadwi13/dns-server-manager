package domain

import (
	"context"
	"github.com/pkg/errors"
)

type ZoneRepository interface {
	GetAllZones(ctx context.Context) ([]*Zone, error)
	GetZoneById(ctx context.Context, zoneId string) (*Zone, error)
	GetZoneByDomain(ctx context.Context, domain string) (*Zone, error)

	Persist(ctx context.Context, zone *Zone) error
	Delete(ctx context.Context, zoneId string) error
}

var ErrorZoneNotFound = errors.New("zone is not found")

type Migration interface {
	Migrate(ctx context.Context) error
}
