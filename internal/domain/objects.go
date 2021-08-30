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
	Id       string
	Domain   string
	FilePath string
	SOA      *SOARecord
	Records  []*Record
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

func (z *Zone) FindRecordyById(recordId string) *Record {
	if recordId == "" {
		return nil
	}
	for _, record := range z.Records {
		if record.Id == recordId {
			return record
		}
	}
	return nil
}

func (z *Zone) FindRecordyByCriteria(name, recordType, value string) []*Record {
	if name == "" && recordType == "" && value == "" {
		return nil
	}
	var records []*Record
	for _, record := range z.Records {
		isMatch := true
		if name != "" && record.Name != name {
			isMatch = false
		}
		if recordType != "" && record.Type != recordType {
			isMatch = false
		}
		if value != "" && record.Value != value {
			isMatch = false
		}
		if isMatch {
			records = append(records, record)
		}
	}
	return records
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

func (z *Zone) DeleteRecord(record *Record) error {
	if record == nil {
		return errors.New("record is not found")
	}
	foundIdx := -1
	for i, r := range z.Records {
		if r == record || r.Id == record.Id {
			foundIdx = i
			break
		}
	}
	if foundIdx == -1 {
		return errors.New("record is not found")
	}
	z.Records[foundIdx] = z.Records[len(z.Records)-1]
	z.Records = z.Records[:len(z.Records)-1]
	return nil
}

func (z *Zone) IsValid() bool {
	return z.Domain != "" && z.FilePath != ""
}

type Record struct {
	Id    string
	Name  string
	Type  string
	Value string
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
	Id                string
	Name              string
	PrimaryNameServer string
	MailAddress       string
	Serial            string
	SerialCounter     int
	Refresh           int
	Retry             int
	Expire            int
	CacheTTL          int
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
