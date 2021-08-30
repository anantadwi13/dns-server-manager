package domain

import (
	"errors"
	"fmt"
	"time"
)

type Validation interface {
	IsValid() bool
}

type Zone struct {
	Id       string     `json:"id"`
	Domain   string     `json:"domain"`
	FilePath string     `json:"file_path"`
	SOA      *SOARecord `json:"soa,omitempty"`
	Records  []*Record  `json:"records,omitempty"`
}

func NewZone(domain string) *Zone {
	return &Zone{Domain: domain}
}

func (z *Zone) RegisterSOA(soa *SOARecord) error {
	if !soa.IsValid() {
		return errors.New("invalid SOA")
	}
	z.SOA = soa
	return nil
}

func (z *Zone) AddRecord(record *Record) error {
	if z.Records != nil {
		for _, r := range z.Records {
			if r == record {
				return errors.New("duplication of record")
			}
			if r.Id == record.Id {
				return errors.New("duplication of record")
			}
			if r.Name == record.Name && r.Type == record.Type && r.Value == record.Value {
				return errors.New("duplication of record")
			}
		}
	}
	z.Records = append(z.Records, record)
	return nil
}

func (z *Zone) DeleteRecord(record *Record) {
	foundIdx := -1
	for i, r := range z.Records {
		if r == record || r.Id == record.Id {
			foundIdx = i
			break
		}
	}
	if foundIdx != -1 {
		z.Records[foundIdx] = z.Records[len(z.Records)-1]
		z.Records = z.Records[:len(z.Records)-1]
	}
}

func (z *Zone) IsValid() bool {
	return z.Domain != "" && z.FilePath != ""
}

type Record struct {
	Id    string `json:"id"`
	Name  string `json:"name"`
	Type  string `json:"type"`
	Value string `json:"value"`
}

func NewRecord(name string, recordType string, value string) *Record {
	return &Record{Name: name, Type: recordType, Value: value}
}

func NewNSRecord(name string, value string) *Record {
	return &Record{Name: name, Type: "NS", Value: value}
}

func (r *Record) IsValid() bool {
	return r.Name != "" && r.Type != "" && r.Value != ""
}

type SOARecord struct {
	Id                string `json:"id"`
	Name              string `json:"name"`
	PrimaryNameServer string `json:"primary_name_server"`
	MailAddress       string `json:"mail_address"`
	Serial            string `json:"serial"`
	SerialCounter     int    `json:"serial_counter"`
	Refresh           int    `json:"refresh"`
	Retry             int    `json:"retry"`
	Expire            int    `json:"expire"`
	CacheTTL          int    `json:"cache_ttl"`
}

func NewDefaultSOARecord(primaryNS, mailAddress string) *SOARecord {
	soa := &SOARecord{
		Name:              "@",
		PrimaryNameServer: primaryNS,
		MailAddress:       mailAddress,
		SerialCounter:     0,
		Refresh:           7200,
		Retry:             3600,
		Expire:            1209600,
		CacheTTL:          180,
	}
	soa.UpdateSerial()
	return soa
}

func (s *SOARecord) UpdateSerial() {
	counter := (s.SerialCounter + 1) % 100
	serial := fmt.Sprintf("%v%02d", time.Now().Format("20060102"), counter)
	s.SerialCounter = counter
	s.Serial = serial
}

func (s *SOARecord) IsValid() bool {
	return s.Name != "" && s.PrimaryNameServer != "" && s.MailAddress != "" &&
		len(s.Serial) == 10 && s.Refresh > 0 && s.Retry > 0 && s.Expire > 0 && s.CacheTTL > 0
}
