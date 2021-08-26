package internal

import (
	"context"
	"database/sql"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"path/filepath"
)

type ZoneRepository interface {
	GetAllZones(ctx context.Context) ([]*Zone, error)
	GetZoneById(ctx context.Context, zoneId string) (*Zone, error)
	GetZoneByDomain(ctx context.Context, domain string) (*Zone, error)

	GetRecordById(ctx context.Context, recordId string) (*Record, error)

	Persist(ctx context.Context, zone *Zone) error
	Delete(ctx context.Context, zoneId string) error
}

var ErrorZoneNotFound = errors.New("zone is not found")

type Migration interface {
	Migrate(ctx context.Context) error
}

type sqliteZoneRepository struct {
	config Config
	db     *sql.DB
}

func NewSqliteZoneRepository(config Config, db *sql.DB) ZoneRepository {
	return &sqliteZoneRepository{config: config, db: db}
}

func (z *sqliteZoneRepository) GetAllZones(ctx context.Context) ([]*Zone, error) {
	zoneRows, err := z.db.QueryContext(ctx, "SELECT * FROM zones;")
	if err != nil {
		return nil, err
	}
	defer zoneRows.Close()

	recordRows, err := z.db.QueryContext(ctx, "SELECT * FROM records;")
	if err != nil {
		return nil, err
	}
	defer recordRows.Close()

	soaRows, err := z.db.QueryContext(ctx, "SELECT * FROM soas;")
	if err != nil {
		return nil, err
	}
	defer soaRows.Close()

	var mapZones = map[string]*Zone{}
	for zoneRows.Next() {
		zone := &Zone{}
		err := zoneRows.Scan(&zone.Id, &zone.Domain, &zone.FilePath)
		if err != nil {
			return nil, err
		}
		z.filePathAssigner(zone)
		mapZones[zone.Id] = zone
	}

	for recordRows.Next() {
		record := &Record{}
		var zoneId string
		err := recordRows.Scan(&record.Id, &zoneId, &record.Name, &record.Type, &record.Value)
		if err != nil {
			return nil, err
		}
		zone, ok := mapZones[zoneId]
		if !ok {
			continue
		}
		zone.Records = append(zone.Records, record)
	}

	for soaRows.Next() {
		soa := &SOARecord{}
		var zoneId string
		err := soaRows.Scan(&soa.Id, &zoneId, &soa.Name, &soa.PrimaryNameServer, &soa.MailAddress, &soa.Serial,
			&soa.SerialCounter, &soa.Refresh, &soa.Retry, &soa.Expire, &soa.CacheTTL)
		if err != nil {
			return nil, err
		}
		zone, ok := mapZones[zoneId]
		if !ok {
			continue
		}
		zone.SOA = soa
	}

	var zones []*Zone
	for _, zone := range mapZones {
		zones = append(zones, zone)
	}
	return zones, nil
}

func (z *sqliteZoneRepository) GetZoneById(ctx context.Context, zoneId string) (*Zone, error) {
	zoneRows, err := z.db.QueryContext(ctx, "SELECT * FROM zones WHERE id = ?;", zoneId)
	if err != nil {
		return nil, err
	}
	defer zoneRows.Close()

	var zone *Zone
	for zoneRows.Next() {
		zone = &Zone{}
		err := zoneRows.Scan(&zone.Id, &zone.Domain, &zone.FilePath)
		if err != nil {
			return nil, err
		}
		break
	}

	if zone == nil {
		return nil, nil
	}
	z.filePathAssigner(zone)

	recordRows, err := z.db.QueryContext(ctx, "SELECT * FROM records;")
	if err != nil {
		return nil, err
	}
	defer recordRows.Close()

	soaRows, err := z.db.QueryContext(ctx, "SELECT * FROM soas;")
	if err != nil {
		return nil, err
	}
	defer soaRows.Close()

	err = z.zonesMapper(zone, recordRows, soaRows)
	if err != nil {
		return nil, err
	}

	return zone, nil
}

func (z *sqliteZoneRepository) GetZoneByDomain(ctx context.Context, domain string) (*Zone, error) {
	zoneRows, err := z.db.QueryContext(ctx, "SELECT * FROM zones WHERE domain = ?;", domain)
	if err != nil {
		return nil, err
	}
	defer zoneRows.Close()

	var zone *Zone
	for zoneRows.Next() {
		zone = &Zone{}
		err := zoneRows.Scan(&zone.Id, &zone.Domain, &zone.FilePath)
		if err != nil {
			return nil, err
		}
		break
	}

	if zone == nil {
		return nil, nil
	}
	z.filePathAssigner(zone)

	recordRows, err := z.db.QueryContext(ctx, "SELECT * FROM records;")
	if err != nil {
		return nil, err
	}
	defer recordRows.Close()

	soaRows, err := z.db.QueryContext(ctx, "SELECT * FROM soas;")
	if err != nil {
		return nil, err
	}
	defer soaRows.Close()

	err = z.zonesMapper(zone, recordRows, soaRows)
	if err != nil {
		return nil, err
	}

	return zone, nil
}

func (z *sqliteZoneRepository) GetRecordById(ctx context.Context, recordId string) (*Record, error) {
	panic("implement me")
}

func (z *sqliteZoneRepository) Persist(ctx context.Context, zone *Zone) (err error) {
	tx, err := z.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	defer func() {
		err = z.finishTransaction(err, tx)
	}()

	if zone.Id == "" {
		zone.Id = uuid.NewString()
	}
	if zone.FilePath == "" {
		z.filePathAssigner(zone)
	}

	_, err = tx.ExecContext(ctx, `
		REPLACE INTO zones(id, domain, file_path) VALUES(?, ?, ?);
	`, zone.Id, zone.Domain, zone.FilePath)
	if err != nil {
		return
	}

	soa := zone.SOA
	if soa != nil {
		if soa.Id == "" {
			soa.Id = uuid.NewString()
		}

		_, err = tx.ExecContext(ctx, `
			REPLACE INTO soas(id, zone_id, name, primary_ns, mail_addr, serial, serial_counter, refresh, retry, expire, cache_ttl) 
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
		`, soa.Id, zone.Id, soa.Name, soa.PrimaryNameServer, soa.MailAddress, soa.Serial, soa.SerialCounter, soa.Refresh, soa.Retry, soa.Expire, soa.CacheTTL)
		if err != nil {
			return
		}
	}

	for _, record := range zone.Records {
		if record.Id == "" {
			record.Id = uuid.NewString()
		}

		_, err = tx.ExecContext(ctx, `
			REPLACE INTO records(id, zone_id, name, type, value) VALUES(?, ?, ?, ?, ?);
		`, record.Id, zone.Id, record.Name, record.Type, record.Value)
		if err != nil {
			return
		}
	}
	return
}

func (z *sqliteZoneRepository) Delete(ctx context.Context, zoneId string) (err error) {
	zone, err := z.GetZoneById(ctx, zoneId)
	if err != nil {
		return err
	}

	if zone == nil {
		return ErrorZoneNotFound
	}

	tx, err := z.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		err = z.finishTransaction(err, tx)
	}()

	_, err = tx.ExecContext(ctx, `
		DELETE FROM zones WHERE id = ?;
		DELETE FROM soas WHERE zone_id = ?;
		DELETE FROM records WHERE zone_id = ?;
	`, zoneId, zoneId, zoneId)

	return
}

func (z *sqliteZoneRepository) finishTransaction(err error, tx *sql.Tx) error {
	if err != nil {
		if rollbackError := tx.Rollback(); rollbackError != nil {
			return errors.Wrap(err, rollbackError.Error())
		}

		return err
	} else {
		if commitError := tx.Commit(); commitError != nil {
			return commitError
		}

		return nil
	}
}

func (z *sqliteZoneRepository) zonesMapper(zone *Zone, recordRows, soaRows *sql.Rows) error {
	for soaRows.Next() {
		soa := &SOARecord{}
		var zoneId string
		err := soaRows.Scan(&soa.Id, &zoneId, &soa.Name, &soa.PrimaryNameServer, &soa.MailAddress, &soa.Serial,
			&soa.SerialCounter, &soa.Refresh, &soa.Retry, &soa.Expire, &soa.CacheTTL)
		if err != nil {
			return err
		}
		zone.SOA = soa
	}

	for recordRows.Next() {
		record := &Record{}
		var zoneId string
		err := recordRows.Scan(&record.Id, &zoneId, &record.Name, &record.Type, &record.Value)
		if err != nil {
			return err
		}
		zone.Records = append(zone.Records, record)
	}
	return nil
}

func (z *sqliteZoneRepository) filePathAssigner(zone *Zone) {
	zone.FilePath = filepath.Join(z.config.BindFolderPath(), "db-"+zone.Domain)
}

type sqliteMigration struct {
	db *sql.DB
}

func NewSqliteMigration(db *sql.DB) Migration {
	return &sqliteMigration{db: db}
}

func (m *sqliteMigration) Migrate(ctx context.Context) error {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS zones (
		    id TEXT PRIMARY KEY,
		    domain TEXT NOT NULL,
		    file_path TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS records (
		    id TEXT PRIMARY KEY,
		    zone_id TEXT NOT NULL,
		    name TEXT NOT NULL,
		    type TEXT NOT NULL,
		    value TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS soas (
		    id TEXT PRIMARY KEY,
		    zone_id TEXT NOT NULL,
		    name TEXT NOT NULL,
		    primary_ns TEXT NOT NULL,
		    mail_addr TEXT NOT NULL,
		    serial TEXT NOT NULL,
		    serial_counter INTEGER,
		    refresh INTEGER NOT NULL,
		    retry INTEGER NOT NULL,
		    expire INTEGER NOT NULL,
		    cache_ttl INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS zones_domain ON zones(domain);
		CREATE INDEX IF NOT EXISTS records_zone_id ON records(zone_id);
		CREATE INDEX IF NOT EXISTS soas_zone_id ON soas(zone_id);
	`)
	if err != nil {
		tx.Rollback()
		return err
	}
	err = tx.Commit()
	if err != nil {
		tx.Rollback()
		return err
	}
	return nil
}
