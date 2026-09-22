package main

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Session struct {
	Timestamp time.Time
	VIN       string
	Model     string
	Dir       string

	MetaFile  string
	TransFile string
	PRGFile   string
	ZipLog    string
	BehDat    string
	FstDat    string

	Meta       *MetaData
	Trans      *TransData
	ZipLogData *ZipLogContents
	FASTA      *FASTAData
}

// MetaData from RG_META_*.xml
type MetaData struct {
	XMLName  xml.Name      `xml:"TransactionMetaData"`
	VIN17    string        `xml:"VIN17"`
	Dealer   string        `xml:"DealerNumber"`
	Features BasicFeatures `xml:"BasicFeatures"`
	Start    string        `xml:"StartDate"`
	End      string        `xml:"EndDate"`
	Mileage  int           `xml:"DistanceOfFastaRead"`
	CommType string        `xml:"VehicleCommunication"`
	State    string        `xml:"WorkState"`
	Computer string        `xml:"ComputerName"`
	CaseID   string        `xml:"IstaCaseId"`
}

type BasicFeatures struct {
	Series          string `xml:"Baureihe"`
	ModelSeries     string `xml:"Ereihe"`
	Body            string `xml:"Karosserie"`
	Engine          string `xml:"Motor"`
	Transmission    string `xml:"Getriebe"`
	AssemblyCountry string `xml:"CountryOfAssembly"`
	Market          string `xml:"BaseVersion"`
	Country         string `xml:"Land"`
	Steering        string `xml:"Lenkung"`
	ModelYear       string `xml:"Modelljahr"`
	ModelMonth      string `xml:"Modellmonat"`
	Brand           string `xml:"Marke"`
	TypeCode        string `xml:"TypeCode"`
}

// TransData from RG_TRANS_*.xml
type TransData struct {
	XMLName xml.Name `xml:"Vehicle"`
	VIN17   string   `xml:"VIN17"`
	Brand   string   `xml:"BrandName"`
	ECUs    []ECU    `xml:"ECU>ECU"`
	ILevel  string   `xml:"ILevel"`
	Mileage int      `xml:"Gwsz"`
	Unit    string   `xml:"GwszUnit"`
}

type ECU struct {
	Title       string `xml:"ECUTitle"`
	Variant     string `xml:"VARIANTE"`
	Bus         string `xml:"BUS"`
	TreeName    string `xml:"TITLE_ECUTREE"`
	ShortName   string `xml:"ECU_GROBNAME"`
	FullName    string `xml:"ECU_NAME"`
	SGBD        string `xml:"ECU_SGBD"`
	Group       string `xml:"ECU_GRUPPE"`
	Protocol    string `xml:"DiagProtocoll"`
	Supplier    string `xml:"ID_LIEF_TEXT"`
	Serial      string `xml:"SERIENNUMMER"`
	MfgDate     string `xml:"ID_DATUM"`
	FaultCount  int    `xml:"F_ANZ"`
	Faults      []DTC  `xml:"FEHLER>DTC"`
	SVK         SVK    `xml:"SVK"`
	CommSuccess string `xml:"COMMUNICATION_SUCCESSFULLY"`
}

type DTC struct {
	UniqueID    string       `xml:"UniqueId"`
	Location    int          `xml:"F_ORT"`
	Description string       `xml:"F_ORT_TEXT"`
	StatusNr    int          `xml:"F_VORHANDEN_NR"`
	StatusText  string       `xml:"F_VORHANDEN_TEXT"`
	ReadyText   string       `xml:"F_READY_TEXT"`
	WarningText string       `xml:"F_WARNUNG_TEXT"`
	PCode       string       `xml:"F_PCODE"`
	SAECode     string       `xml:"F_SAE_CODE"`
	HexCode     string       `xml:"F_HEX_CODE"`
	Context     []DTCContext `xml:"DTCContext>typeDTCContext"`
}

type DTCContext struct {
	Mileage   int `xml:"F_UW_KM"`
	FaultTime int `xml:"F_UW_ZEIT"`
	Count     int `xml:"F_UW_ANZ"`
}

type SVK struct {
	SGBMIDs  []string `xml:"XWE_SGBMID>string"`
	ProgDate string   `xml:"PROG_DATUM"`
}

func discoverSessions(cfg Config) ([]Session, error) {
	var sessions []Session

	transDir := filepath.Join(cfg.ISTA.InstallDir, "Transactions")
	logsDir := cfg.ISTA.LogDir
	fastaDir := filepath.Join(cfg.ISTA.InstallDir, "FASTAOut")

	metaFiles, _ := filepath.Glob(filepath.Join(transDir, "RG_META_*.xml"))
	for _, metaFile := range metaFiles {
		base := filepath.Base(metaFile)
		// RG_META_<VIN17><YYYYMMDD><HHMMSS>.xml
		rest := strings.TrimPrefix(base, "RG_META_")
		rest = strings.TrimSuffix(rest, ".xml")
		if len(rest) < 31 {
			continue
		}
		vin := rest[:17]
		dateStr := rest[17:25]
		timeStr := rest[25:31]

		ts, err := time.Parse("20060102150405", dateStr+timeStr)
		if err != nil {
			continue
		}

		s := Session{
			Timestamp: ts,
			VIN:       vin,
			MetaFile:  metaFile,
		}

		prefix := fmt.Sprintf("RG_TRANS_%s%s%s", vin, dateStr, timeStr)
		s.TransFile = filepath.Join(transDir, prefix+".xml")
		if _, err := os.Stat(s.TransFile); err != nil {
			s.TransFile = ""
		}

		prefix = fmt.Sprintf("RG_PRG_%s%s%s", vin, dateStr, timeStr)
		s.PRGFile = filepath.Join(transDir, prefix+".xml")
		if _, err := os.Stat(s.PRGFile); err != nil {
			s.PRGFile = ""
		}

		// zip.log: YYYY-MM-DD_HHMMSS_MODEL_VIN.zip.log
		zipPattern := filepath.Join(logsDir, fmt.Sprintf("%s-%s-%s_%s_*_%s.zip.log",
			dateStr[:4], dateStr[4:6], dateStr[6:8], timeStr, vin))
		if matches, _ := filepath.Glob(zipPattern); len(matches) > 0 {
			s.ZipLog = matches[0]
			// extract model from filename
			base := filepath.Base(matches[0])
			parts := strings.Split(strings.TrimSuffix(base, ".zip.log"), "_")
			if len(parts) >= 3 {
				s.Model = parts[2]
			}
		}

		// FASTAOut: 1_VIN_DEALER_DPNR_XX_YYYYMMDD_HHMMSS.{behdat,fstdat}
		fastaPattern := filepath.Join(fastaDir, fmt.Sprintf("*_%s_*_%s_%s.*dat", vin, dateStr, timeStr))
		if matches, _ := filepath.Glob(fastaPattern); len(matches) > 0 {
			for _, m := range matches {
				switch {
				case strings.HasSuffix(m, ".behdat"):
					s.BehDat = m
				case strings.HasSuffix(m, ".fstdat"):
					s.FstDat = m
				}
			}
		}

		sessions = append(sessions, s)
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].Timestamp.After(sessions[j].Timestamp)
	})

	return sessions, nil
}

func (s *Session) ParseMeta() error {
	if s.MetaFile == "" {
		return fmt.Errorf("no meta file")
	}
	data, err := os.ReadFile(s.MetaFile)
	if err != nil {
		return err
	}
	s.Meta = &MetaData{}
	return xml.Unmarshal(data, s.Meta)
}

func (s *Session) ParseTrans() error {
	if s.TransFile == "" {
		return fmt.Errorf("no trans file")
	}
	data, err := os.ReadFile(s.TransFile)
	if err != nil {
		return err
	}
	s.Trans = &TransData{}
	return xml.Unmarshal(data, s.Trans)
}

func (s *Session) AllFaults() []ECUFault {
	if s.Trans == nil {
		return nil
	}
	var faults []ECUFault
	for _, ecu := range s.Trans.ECUs {
		for _, dtc := range ecu.Faults {
			faults = append(faults, ECUFault{
				ECUName:     ecu.TreeName,
				ECUFullName: ecu.FullName,
				Bus:         ecu.Bus,
				DTC:         dtc,
			})
		}
	}
	return faults
}

type ECUFault struct {
	ECUName     string
	ECUFullName string
	Bus         string
	DTC         DTC
}

func (s *Session) ParseZipLog() error {
	if s.ZipLog == "" {
		return fmt.Errorf("no zip.log file")
	}
	zlc, err := parseZipLog(s.ZipLog)
	if err != nil {
		return err
	}
	s.ZipLogData = zlc
	return nil
}

func (s *Session) ParseFASTA() error {
	s.FASTA = &FASTAData{}
	if s.FstDat != "" {
		tests, err := parseFSTATests(s.FstDat)
		if err == nil {
			s.FASTA.Tests = tests
		}
	}
	if s.BehDat != "" {
		actions, err := parseFSTABehavior(s.BehDat)
		if err == nil {
			s.FASTA.Actions = actions
		}
	}
	if len(s.FASTA.Tests) == 0 && len(s.FASTA.Actions) == 0 {
		s.FASTA = nil
		return fmt.Errorf("no FASTA data found")
	}
	return nil
}
