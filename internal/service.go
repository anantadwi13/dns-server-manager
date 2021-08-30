package internal

import (
	"context"
	"database/sql"
	"github.com/anantadwi13/dns-server-manager/internal/domain"
	"github.com/anantadwi13/dns-server-manager/internal/external"
	"github.com/labstack/echo/v4"
	_ "github.com/mattn/go-sqlite3"
	"github.com/pkg/errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

type service struct {
	config         domain.Config
	e              *echo.Echo
	db             *sql.DB
	migration      domain.Migration
	zoneRepository domain.ZoneRepository
	bindHelper     domain.DNSServer
	shutdownWg     sync.WaitGroup
}

func NewService(config domain.Config) *service {
	return &service{config: config}
}

func (s *service) Start() {
	ctx := context.Background()
	signalOS := make(chan os.Signal, 1)
	signal.Notify(signalOS, syscall.SIGINT, syscall.SIGTERM)

	s.registerDependencies(ctx)

	s.loadBindService(ctx)

	s.loadAPIServer(ctx)

	select {
	case <-signalOS:
		log.Println("Service is stopping")
		s.gracefulShutdown(ctx)
		s.shutdownWg.Wait()
		log.Println("Service is stopped")
	}
}

func (s *service) registerDependencies(ctx context.Context) {
	s.e = echo.New()

	err := os.MkdirAll(s.config.DataFolderPath(), 0777)
	if err != nil {
		log.Panicln(err)
	}
	s.db, err = sql.Open("sqlite3", s.config.DBPath())
	if err != nil {
		log.Panicln(err)
	}

	s.migration = external.NewSqliteMigration(s.db)
	err = s.migration.Migrate(ctx)
	if err != nil {
		log.Panicln(err)
	}

	s.zoneRepository = external.NewSqliteZoneRepository(s.config, s.db)

	s.bindHelper = external.NewBind9Server(s.config, s.zoneRepository)
}

func (s *service) loadBindService(ctx context.Context) {
	err := s.bindHelper.UpdateAndReload(ctx)
	if err != nil {
		log.Panicln(err)
	}
}

func (s *service) loadAPIServer(ctx context.Context) {
	go func() {
		external.RegisterHandlers(s.e, s)
		err := s.e.Start(":5555")
		if err != nil && err != http.ErrServerClosed {
			log.Fatalf("shutting down the server %v\n", err)
		}
	}()
}

func (s *service) gracefulShutdown(ctx context.Context) {
	go func() {
		s.shutdownWg.Add(1)
		defer s.shutdownWg.Done()
		err := s.bindHelper.Shutdown(ctx)
		if err != nil {
			log.Fatalln(err)
		}
	}()
	go func() {
		s.shutdownWg.Add(1)
		defer s.shutdownWg.Done()
		err := s.e.Shutdown(ctx)
		if err != nil {
			log.Fatalln(err)
		}
	}()
	go func() {
		s.shutdownWg.Add(1)
		defer s.shutdownWg.Done()
		err := s.db.Close()
		if err != nil {
			log.Fatalln(err)
		}
	}()
}

func (s *service) GetRecordsDomain(c echo.Context, domainName string) error {
	zone, err := s.zoneRepository.GetZoneByDomain(c.Request().Context(), domainName)
	if err != nil {
		return responseServerErr(c, err)
	}
	if zone == nil {
		return responseNotFound(c, "zone is not found")
	}

	var recordsRes = make([]*external.RecordRes, 0)
	for _, record := range zone.Records {
		recordsRes = append(recordsRes, recordMapper(record))
	}

	return c.JSON(http.StatusOK, recordsRes)
}

func (s *service) PostRecordsDomain(c echo.Context, domainName string) error {
	name := c.FormValue("name")
	recordType := c.FormValue("type")
	value := c.FormValue("value")

	if name == "" || recordType == "" || value == "" {
		return responseClientErr(c, errors.New("make sure name, type, value are set"))
	}

	zone, err := s.zoneRepository.GetZoneByDomain(c.Request().Context(), domainName)
	if err != nil {
		return responseServerErr(c, err)
	}
	if zone == nil {
		return responseNotFound(c, "zone is not found")
	}

	record := domain.NewRecord(name, recordType, value)

	err = zone.AddRecord(record)
	if err != nil {
		return responseClientErr(c, err)
	}

	err = s.zoneRepository.Persist(c.Request().Context(), zone)
	if err != nil {
		return responseServerErr(c, err)
	}

	err = s.bindHelper.UpdateAndReload(c.Request().Context())
	if err != nil {
		return responseServerErr(c, err)
	}

	return c.JSON(http.StatusCreated, recordMapper(record))
}

func (s *service) DeleteRecordsDomainRecordId(c echo.Context, domainName string, recordId string) error {
	zone, err := s.zoneRepository.GetZoneByDomain(c.Request().Context(), domainName)
	if err != nil {
		return responseServerErr(c, err)
	}
	if zone == nil {
		return responseNotFound(c, "zone is not found")
	}

	record := zone.FindRecordyById(recordId)
	if record == nil {
		return responseNotFound(c, "record is not found")
	}

	err = zone.DeleteRecord(record)
	if err != nil {
		return responseClientErr(c, err)
	}

	err = s.zoneRepository.Persist(c.Request().Context(), zone)
	if err != nil {
		return responseServerErr(c, err)
	}

	err = s.bindHelper.UpdateAndReload(c.Request().Context())
	if err != nil {
		return responseServerErr(c, err)
	}

	return responseOk(c, "OK")
}

func (s *service) GetRecordsDomainRecordId(c echo.Context, domainName string, recordId string) error {
	zone, err := s.zoneRepository.GetZoneByDomain(c.Request().Context(), domainName)
	if err != nil {
		return responseServerErr(c, err)
	}
	if zone == nil {
		return responseNotFound(c, "zone is not found")
	}

	record := zone.FindRecordyById(recordId)
	if record == nil {
		return responseNotFound(c, "record is not found")
	}

	return c.JSON(http.StatusOK, recordMapper(record))
}

func (s *service) PutRecordsDomainRecordId(c echo.Context, domainName string, recordId string) error {
	name := c.FormValue("name")
	recordType := c.FormValue("type")
	value := c.FormValue("value")

	zone, err := s.zoneRepository.GetZoneByDomain(c.Request().Context(), domainName)
	if err != nil {
		return responseServerErr(c, err)
	}
	if zone == nil {
		return responseNotFound(c, "zone is not found")
	}

	record := zone.FindRecordyById(recordId)
	if record == nil {
		return responseNotFound(c, "record is not found")
	}

	if name != "" {
		record.Name = name
	}
	if recordType != "" {
		record.Type = recordType
	}
	if value != "" {
		record.Value = value
	}

	if !record.IsValid() {
		return responseClientErr(c, errors.New("record is not valid"))
	}

	err = s.zoneRepository.Persist(c.Request().Context(), zone)
	if err != nil {
		return responseServerErr(c, err)
	}

	err = s.bindHelper.UpdateAndReload(c.Request().Context())
	if err != nil {
		return responseServerErr(c, err)
	}

	return c.JSON(http.StatusOK, recordMapper(record))
}

func (s *service) GetZones(c echo.Context) error {
	zones, err := s.zoneRepository.GetAllZones(c.Request().Context())
	if err != nil {
		return err
	}

	zonesRes := make([]*external.ZoneRes, 0)
	for _, zone := range zones {
		zonesRes = append(zonesRes, zoneMapper(zone))
	}
	return c.JSON(http.StatusOK, zonesRes)
}

func (s *service) PostZones(c echo.Context) error {
	domainReq := c.FormValue("domain")
	primaryNSReq := c.FormValue("primary_ns")
	mailAddrReq := c.FormValue("mail_addr")

	if domainReq == "" || primaryNSReq == "" || mailAddrReq == "" {
		return responseClientErr(c, errors.New("make sure domain, primary_ns, and mail_addr are set"))
	}

	zoneExist, err := s.zoneRepository.GetZoneByDomain(c.Request().Context(), domainReq)
	if err != nil {
		return responseServerErr(c, err)
	}
	if zoneExist != nil {
		return responseClientErr(c, errors.New("zone already exists"))
	}

	zone := domain.NewZone(domainReq)

	err = zone.RegisterSOA(domain.NewDefaultSOARecord(primaryNSReq, mailAddrReq))
	if err != nil {
		return responseClientErr(c, err)
	}

	err = zone.AddRecord(domain.NewNSRecord("@", primaryNSReq))
	if err != nil {
		return responseClientErr(c, err)
	}

	err = s.zoneRepository.Persist(c.Request().Context(), zone)
	if err != nil {
		return responseServerErr(c, err)
	}

	err = s.bindHelper.UpdateAndReload(c.Request().Context())
	if err != nil {
		return responseServerErr(c, err)
	}

	return c.JSON(http.StatusCreated, zoneMapper(zone))
}

func (s *service) DeleteZonesDomain(c echo.Context, domainName string) error {
	ctx := c.Request().Context()

	zone, err := s.zoneRepository.GetZoneByDomain(ctx, domainName)
	if err != nil {
		return responseServerErr(c, err)
	}
	if zone == nil {
		return responseNotFound(c, "zone is not found")
	}

	err = s.zoneRepository.Delete(c.Request().Context(), zone)
	if err != nil {
		return responseServerErr(c, err)
	}

	err = s.bindHelper.UpdateAndReload(c.Request().Context())
	if err != nil {
		return responseServerErr(c, err)
	}

	return responseOk(c, "OK")
}

func (s *service) GetZonesDomain(c echo.Context, domainName string) error {
	zone, err := s.zoneRepository.GetZoneByDomain(c.Request().Context(), domainName)
	if err != nil {
		return responseClientErr(c, err)
	}
	if zone == nil {
		return responseNotFound(c, "zone is not found")
	}

	return c.JSON(http.StatusOK, zoneMapper(zone))
}

func (s *service) PutZonesDomain(c echo.Context, domainName string) error {
	ctx := c.Request().Context()
	domainReq := c.FormValue("domain")
	primaryNSReq := c.FormValue("primary_ns")
	mailAddrReq := c.FormValue("mail_addr")

	zone, err := s.zoneRepository.GetZoneByDomain(ctx, domainName)
	if err != nil {
		return responseServerErr(c, err)
	}
	if zone == nil {
		return responseNotFound(c, "zone is not found")
	}

	if domainReq != "" {
		zone.Domain = domainReq
	}
	if primaryNSReq != "" {
		zone.SOA.PrimaryNameServer = primaryNSReq
	}
	if mailAddrReq != "" {
		zone.SOA.MailAddress = mailAddrReq
	}

	if !zone.IsValid() {
		return responseClientErr(c, errors.New("zone input(s) are not valid"))
	}

	err = s.zoneRepository.Persist(ctx, zone)
	if err != nil {
		return responseServerErr(c, err)
	}

	err = s.bindHelper.UpdateAndReload(ctx)
	if err != nil {
		return responseServerErr(c, err)
	}

	return c.JSON(http.StatusOK, zoneMapper(zone))
}

func responseOk(c echo.Context, message string) error {
	return c.JSON(http.StatusOK, external.GeneralRes{
		Code:    http.StatusOK,
		Message: message,
	})
}
func responseNotFound(c echo.Context, message string) error {
	return c.JSON(http.StatusNotFound, external.GeneralRes{
		Code:    http.StatusNotFound,
		Message: message,
	})
}

func responseServerErr(c echo.Context, err error) error {
	return c.JSON(http.StatusInternalServerError, external.GeneralRes{
		Code:    http.StatusInternalServerError,
		Message: err.Error(),
	})
}

func responseClientErr(c echo.Context, err error) error {
	return c.JSON(http.StatusBadRequest, external.GeneralRes{
		Code:    http.StatusBadRequest,
		Message: err.Error(),
	})
}

func zoneMapper(zone *domain.Zone) *external.ZoneRes {
	if zone == nil {
		return nil
	}
	var records []external.RecordRes
	for _, record := range zone.Records {
		records = append(records, *recordMapper(record))
	}
	return &external.ZoneRes{
		Domain:  zone.Domain,
		Id:      zone.Id,
		Records: records,
		Soa:     *soaMapper(zone.SOA),
	}
}

func recordMapper(record *domain.Record) *external.RecordRes {
	if record == nil {
		return nil
	}
	return &external.RecordRes{
		Id:    record.Id,
		Name:  record.Name,
		Type:  external.RecordResType(record.Type),
		Value: record.Value,
	}
}

func soaMapper(soa *domain.SOARecord) *external.SoaRes {
	if soa == nil {
		return nil
	}
	return &external.SoaRes{
		Id:                soa.Id,
		MailAddress:       soa.MailAddress,
		Name:              soa.Name,
		PrimaryNameServer: soa.PrimaryNameServer,
		Refresh:           soa.Refresh,
		Retry:             soa.Retry,
		Serial:            soa.Serial,
		Expire:            soa.Expire,
		CacheTtl:          soa.CacheTTL,
	}
}
