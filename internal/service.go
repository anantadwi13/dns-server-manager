package internal

import (
	"context"
	"database/sql"
	"github.com/labstack/echo/v4"
	_ "github.com/mattn/go-sqlite3"
	"github.com/pkg/errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type service struct {
	config         Config
	e              *echo.Echo
	db             *sql.DB
	migration      Migration
	zoneRepository ZoneRepository
	bindHelper     DNSServer
}

func NewService(config Config) *service {
	return &service{config: config}
}

func (s *service) Start() {
	ctx := context.Background()
	signalOS := make(chan os.Signal, 1)
	signal.Notify(signalOS, syscall.SIGINT, syscall.SIGTERM)

	s.registerDependencies(ctx)

	s.registerRoute()

	s.loadBindService(ctx)

	s.loadAPIServer(ctx)

	select {
	case <-signalOS:
		log.Println("Service is stopping")
		s.gracefulShutdown(ctx)
		time.Sleep(3 * time.Second)
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

	s.migration = NewSqliteMigration(s.db)
	err = s.migration.Migrate(ctx)
	if err != nil {
		log.Panicln(err)
	}

	s.zoneRepository = NewSqliteZoneRepository(s.config, s.db)

	s.bindHelper = NewBind9Server(s.config, s.zoneRepository)
}

func (s *service) registerRoute() {
	s.e.GET("/zones", s.handleListZones)
	s.e.GET("/zone/:zone_id", s.handleDetailZone)
	s.e.POST("/zone", s.handleCreateZone)
	s.e.PUT("/zone/:zone_id", s.handleUpdateZone)
	s.e.DELETE("/zone/:zone_id", s.handleDeleteZone)

	s.e.GET("/record/:zone_id", s.handleListRecords)
	s.e.GET("/record/:zone_id/:record_id", s.handleDetailRecord)
	s.e.POST("/record/:zone_id", s.handleCreateRecord)
	s.e.PUT("/record/:zone_id/:record_id", s.handleUpdateRecord)
	s.e.DELETE("/record/:zone_id/:record_id", s.handleDeleteRecord)
}

func (s *service) loadBindService(ctx context.Context) {
	err := s.bindHelper.UpdateAndReload(ctx)
	if err != nil {
		log.Panicln(err)
	}
}

func (s *service) loadAPIServer(ctx context.Context) {
	go func() {
		err := s.e.Start(":80")
		if err != nil && err != http.ErrServerClosed {
			log.Fatalf("shutting down the server %v\n", err)
		}
	}()
}

func (s *service) gracefulShutdown(ctx context.Context) {
	go func() {
		err := s.bindHelper.Shutdown(ctx)
		if err != nil {
			log.Fatalln(err)
		}
	}()
	go func() {
		err := s.e.Shutdown(ctx)
		if err != nil {
			log.Fatalln(err)
		}
	}()
	go func() {
		err := s.db.Close()
		if err != nil {
			log.Fatalln(err)
		}
	}()
}

func (s *service) handleListZones(c echo.Context) error {
	zones, err := s.zoneRepository.GetAllZones(c.Request().Context())
	if err != nil {
		return err
	}
	if zones == nil {
		zones = []*Zone{}
	}
	return c.JSON(http.StatusOK, zones)
}

func (s *service) handleDetailZone(c echo.Context) error {
	zoneId := c.Param("zone_id")

	zone, err := s.zoneRepository.GetZoneById(c.Request().Context(), zoneId)
	if err != nil {
		return responseClientErr(c, err)
	}
	if zone == nil {
		return c.JSON(http.StatusNotFound, MessageResponse{"zone is not found"})
	}

	return c.JSON(http.StatusOK, zone)
}

func (s *service) handleCreateZone(c echo.Context) error {
	domain := c.FormValue("domain")
	primaryNS := c.FormValue("primary_ns")
	mailAddr := c.FormValue("mail_addr")

	if domain == "" || primaryNS == "" || mailAddr == "" {
		return responseClientErr(c, errors.New("make sure domain, primary_ns, and mail_addr are set"))
	}

	zoneExist, err := s.zoneRepository.GetZoneByDomain(c.Request().Context(), domain)
	if err != nil {
		return responseClientErr(c, err)
	}
	if zoneExist != nil {
		return responseClientErr(c, errors.New("zone already exists"))
	}

	zone := NewZone(domain)

	err = zone.RegisterSOA(NewDefaultSOARecord(primaryNS, mailAddr))
	if err != nil {
		return responseClientErr(c, err)
	}

	err = zone.AddRecord(NewNSRecord("@", primaryNS))
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

	return c.JSON(http.StatusOK, MessageResponse{"ok"})
}

func (s *service) handleUpdateZone(c echo.Context) error {
	ctx := c.Request().Context()
	zoneId := c.Param("zone_id")
	domain := c.FormValue("domain")
	primaryNS := c.FormValue("primary_ns")
	mailAddr := c.FormValue("mail_addr")
	//filePath := c.FormValue("file_path")

	zone, err := s.zoneRepository.GetZoneById(ctx, zoneId)
	if err != nil {
		return responseClientErr(c, err)
	}
	if zone == nil {
		return responseNotFound(c, "zone is not found")
	}

	if domain != "" {
		zone.Domain = domain
	}
	if primaryNS != "" {
		zone.SOA.PrimaryNameServer = primaryNS
	}
	if mailAddr != "" {
		zone.SOA.MailAddress = mailAddr
	}
	//if filePath != "" {
	//	zone.FilePath = filepath.Clean(filePath)
	//}

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

	return c.JSON(http.StatusOK, MessageResponse{"ok"})
}

func (s *service) handleDeleteZone(c echo.Context) error {
	zoneId := c.Param("zone_id")

	err := s.zoneRepository.Delete(c.Request().Context(), zoneId)
	if err != nil {
		if errors.Is(err, ErrorZoneNotFound) {
			return responseNotFound(c, "zone is not found")
		}
		return responseServerErr(c, err)
	}

	err = s.bindHelper.UpdateAndReload(c.Request().Context())
	if err != nil {
		return responseServerErr(c, err)
	}

	return c.JSON(http.StatusOK, MessageResponse{"ok"})
}

func (s *service) handleListRecords(c echo.Context) error {
	zoneId := c.Param("zone_id")

	zone, err := s.zoneRepository.GetZoneById(c.Request().Context(), zoneId)
	if err != nil {
		return responseClientErr(c, err)
	}

	if zone == nil {
		return c.JSON(http.StatusNotFound, MessageResponse{"zone is not found"})
	}

	if len(zone.Records) <= 0 {
		return c.JSON(http.StatusOK, []*Record{})
	}

	return c.JSON(http.StatusOK, zone.Records)
}

func (s *service) handleDetailRecord(c echo.Context) error {
	zoneId := c.Param("zone_id")
	recordId := c.Param("record_id")

	zone, err := s.zoneRepository.GetZoneById(c.Request().Context(), zoneId)
	if err != nil {
		return responseClientErr(c, err)
	}
	if zone == nil {
		return c.JSON(http.StatusNotFound, MessageResponse{"zone is not found"})
	}

	for _, record := range zone.Records {
		if record.Id == recordId {
			return c.JSON(http.StatusOK, record)
		}
	}

	return c.JSON(http.StatusNotFound, MessageResponse{"record is not found!"})
}

func (s *service) handleCreateRecord(c echo.Context) error {
	zoneId := c.Param("zone_id")
	name := c.FormValue("name")
	recordType := c.FormValue("type")
	value := c.FormValue("value")

	if name == "" || recordType == "" || value == "" {
		return responseClientErr(c, errors.New("make sure name, type, value are set"))
	}

	zone, err := s.zoneRepository.GetZoneById(c.Request().Context(), zoneId)
	if err != nil {
		return responseClientErr(c, err)
	}
	if zone == nil {
		return c.JSON(http.StatusNotFound, MessageResponse{"zone is not found"})
	}

	record := NewRecord(name, recordType, value)

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

	return c.JSON(http.StatusOK, MessageResponse{"ok"})
}

func (s *service) handleUpdateRecord(c echo.Context) error {
	zoneId := c.Param("zone_id")
	recordId := c.Param("record_id")
	name := c.FormValue("name")
	recordType := c.FormValue("type")
	value := c.FormValue("value")

	zone, err := s.zoneRepository.GetZoneById(c.Request().Context(), zoneId)
	if err != nil {
		return responseClientErr(c, err)
	}
	if zone == nil {
		return responseNotFound(c, "zone is not found")
	}

	var record *Record

	for _, r := range zone.Records {
		if r.Id == recordId {
			record = r
			break
		}
	}

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

	return c.JSON(http.StatusOK, MessageResponse{"ok"})
}

func (s *service) handleDeleteRecord(c echo.Context) error {
	zoneId := c.Param("zone_id")
	recordId := c.Param("record_id")

	zone, err := s.zoneRepository.GetZoneById(c.Request().Context(), zoneId)
	if err != nil {
		return responseClientErr(c, err)
	}
	if zone == nil {
		return responseNotFound(c, "zone is not found")
	}

	var record *Record
	for _, r := range zone.Records {
		if r.Id == recordId {
			record = r
			break
		}
	}
	if record == nil {
		return responseClientErr(c, errors.New("record is not found"))
	}

	zone.DeleteRecord(record)

	err = s.zoneRepository.Persist(c.Request().Context(), zone)
	if err != nil {
		return responseServerErr(c, err)
	}

	err = s.bindHelper.UpdateAndReload(c.Request().Context())
	if err != nil {
		return responseServerErr(c, err)
	}

	return c.JSON(http.StatusOK, MessageResponse{"ok"})
}

func responseNotFound(c echo.Context, message string) error {
	return c.JSON(http.StatusBadGateway, MessageResponse{message})
}

func responseServerErr(c echo.Context, err error) error {
	return c.JSON(http.StatusBadGateway, MessageResponse{err.Error()})
}

func responseClientErr(c echo.Context, err error) error {
	return c.JSON(http.StatusBadRequest, MessageResponse{err.Error()})
}

type MessageResponse struct {
	Message string `json:"message"`
}
