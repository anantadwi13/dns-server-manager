package internal

import (
	"bufio"
	"context"
	"fmt"
	"github.com/pkg/errors"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

type bind9Server struct {
	config         Config
	zoneRepo       ZoneRepository
	numLock        sync.RWMutex
	numCmds        int
	shutdownSignal chan int
	reloadSignal   chan int
}

func NewBind9Server(config Config, zoneRepo ZoneRepository) DNSServer {
	return &bind9Server{
		config:         config,
		zoneRepo:       zoneRepo,
		shutdownSignal: make(chan int, 1),
		reloadSignal:   make(chan int, 1),
	}
}

func (b *bind9Server) UpdateConfigs(ctx context.Context) error {
	zones, err := b.zoneRepo.GetAllZones(ctx)
	if err != nil {
		return err
	}
	err = b.generateNamedConf(zones)
	if err != nil {
		return err
	}
	err = b.generateDbRecords(ctx, zones)
	if err != nil {
		return err
	}
	return nil
}

func (b *bind9Server) Reload(ctx context.Context) error {
	cmd := exec.Command("/usr/sbin/named", "-g", "-c", b.config.NamedConfPath(), "-u", "bind")
	logs, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	b.numLock.RLock()
	numCmds := b.numCmds
	b.numLock.RUnlock()
	for i := 0; i < numCmds; i++ {
		b.reloadSignal <- 1
	}

	done := make(chan error, 1)

	go func() {
		err = cmd.Start()
		log.Println("Start Bind9")
		if err != nil {
			log.Fatalln(err)
		}

		b.numLock.Lock()
		b.numCmds++
		b.numLock.Unlock()

		scanner := bufio.NewScanner(logs)
		for scanner.Scan() {
			m := scanner.Text()
			log.Println(m)
		}

		done <- cmd.Wait()
	}()

	go func() {
		select {
		case <-b.shutdownSignal:
			if err := cmd.Process.Kill(); err != nil {
				log.Fatalln(err)
			}
			log.Println("Shutdown Bind9")
		case <-b.reloadSignal:
			if err := cmd.Process.Kill(); err != nil {
				log.Fatalln(err)
			}
			log.Println("Reload Bind9")
		case err := <-done:
			if err != nil {
				log.Fatalln(err)
			}
			log.Println("Exit Bind9")
		}
		b.numLock.Lock()
		b.numCmds -= 1
		b.numLock.Unlock()
	}()
	return err
}

func (b *bind9Server) UpdateAndReload(ctx context.Context) error {
	err := b.UpdateConfigs(ctx)
	if err != nil {
		return err
	}
	err = b.Reload(ctx)
	if err != nil {
		return err
	}
	return nil
}

func (b *bind9Server) Shutdown(ctx context.Context) error {
	b.numLock.RLock()
	numCmds := b.numCmds
	b.numLock.RUnlock()
	for i := 0; i < numCmds; i++ {
		b.shutdownSignal <- 1
	}
	return nil
}

func (b *bind9Server) generateNamedConf(zones []*Zone) error {
	fileContents := fmt.Sprintf(`include "%v"; include "%v"; include "%v";`+"\n",
		filepath.Join(b.config.BindFolderPath(), "named.conf.options"),
		filepath.Join(b.config.BindFolderPath(), "named.conf.local"),
		filepath.Join(b.config.BindFolderPath(), "named.conf.default-zones"))
	zoneFormat := `zone "%v" {type primary; file "%v";};` + "\n"
	for _, zone := range zones {
		if !zone.IsValid() {
			continue
		}
		fileContents += fmt.Sprintf(zoneFormat, zone.Domain, zone.FilePath)
	}

	err := writeFile(b.config.NamedConfPath(), fileContents)
	if err != nil {
		return err
	}
	return nil
}

func (b *bind9Server) generateDbRecords(ctx context.Context, zones []*Zone) (err error) {
	soaFormat := `%v	IN	SOA     %v %v (
						%v				; Serial 2021082501
						%v				; Refresh 7200
						%v				; Retry 3600
						%v				; Expire 1209600
						%v )			; Negative Cache TTL 180` + "\n"
	recordFormat := "%v	IN	%v	%v\n"

	for _, zone := range zones {
		fileContents := "$TTL    14400\n"
		soa := zone.SOA
		if soa == nil {
			continue
		}
		soa.UpdateSerial()
		if !soa.IsValid() {
			continue // Skip current zone records because of invalid SOA
		}
		fileContents += fmt.Sprintf(soaFormat, soa.Name, soa.PrimaryNameServer, soa.MailAddress, soa.Serial, soa.Refresh, soa.Retry, soa.Expire, soa.CacheTTL)

		for _, record := range zone.Records {
			if !record.IsValid() {
				continue
			}
			fileContents += fmt.Sprintf(recordFormat, record.Name, record.Type, record.Value)
		}

		errTemp := b.zoneRepo.Persist(ctx, zone)
		if errTemp != nil {
			err = errors.Wrap(errTemp, err.Error())
			continue
		}

		errTemp = writeFile(zone.FilePath, fileContents)
		if errTemp != nil {
			err = errors.Wrap(errTemp, err.Error())
			continue
		}
	}
	return
}

func writeFile(filePath, fileContents string) error {
	err := os.MkdirAll(filepath.Dir(filePath), 0777)
	if err != nil {
		return err
	}
	err = os.WriteFile(filePath, []byte(fileContents), 0666)
	if err != nil {
		return err
	}
	return nil
}
