// Package fronius kapselt die Fronius Solar API v1 (lokales HTTP/JSON auf dem
// GEN24-Wechselrichter, unauthentifiziert lesbar). Alle Endpunkte/Feldnamen in
// dieser Datei sind live gegen ein echtes Geraet (Fronius GEN24 12.0 SC + Reserva,
// 2026-09-17) verifiziert - siehe Plan-Datei fuer die rohen curl-Mitschnitte,
// NICHT aus der PDF-Doku geraten.
package fronius

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

type Client struct {
	baseURL string
	http    *http.Client
}

func NewClient(host string, timeout time.Duration) *Client {
	return &Client{
		baseURL: "http://" + host,
		http:    &http.Client{Timeout: timeout},
	}
}

// envelope ist die gemeinsame Huelle aller /solar_api/v1/-Endpunkte
// (Get*.cgi/.fcgi) - NICHT von GetAPIVersion.cgi verwendet, siehe
// GetAPIVersion() unten.
type envelope struct {
	Body struct {
		Data json.RawMessage `json:"Data"`
	} `json:"Body"`
	Head struct {
		Status struct {
			Code   int    `json:"Code"`
			Reason string `json:"Reason"`
		} `json:"Status"`
	} `json:"Head"`
}

func (c *Client) get(path string, query url.Values) (json.RawMessage, error) {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	resp, err := c.http.Get(u)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: HTTP %d", path, resp.StatusCode)
	}
	var env envelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return nil, fmt.Errorf("GET %s: Antwort nicht lesbar: %w", path, err)
	}
	// Status.Code 11 = "request is not supported" (z.B. GetLoggerInfo.cgi auf
	// diesem Geraet, live bestaetigt) - jeder Nicht-Null-Code wird als Fehler
	// behandelt, der Aufrufer (poller.go) ueberspringt dann nur diesen
	// Teil-Abruf statt den ganzen Zyklus abzubrechen.
	if env.Head.Status.Code != 0 {
		return nil, fmt.Errorf("GET %s: Fronius meldet Status %d: %s", path, env.Head.Status.Code, env.Head.Status.Reason)
	}
	return env.Body.Data, nil
}

// APIVersion / GetAPIVersion.cgi liegt (live bestaetigt) NICHT hinter
// /v1/ und NICHT in der Body/Head-Huelle - eigenes, einfaches Decoding.
type APIVersion struct {
	APIVersion         int    `json:"APIVersion"`
	BaseURL            string `json:"BaseURL"`
	CompatibilityRange string `json:"CompatibilityRange"`
}

func (c *Client) GetAPIVersion() (*APIVersion, error) {
	resp, err := c.http.Get(c.baseURL + "/solar_api/GetAPIVersion.cgi")
	if err != nil {
		return nil, fmt.Errorf("GetAPIVersion: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GetAPIVersion: HTTP %d", resp.StatusCode)
	}
	var v APIVersion
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return nil, fmt.Errorf("GetAPIVersion: Antwort nicht lesbar: %w", err)
	}
	return &v, nil
}

// --- Geraete-Discovery (GetActiveDeviceInfo.cgi?DeviceClass=System) --------
// Live bestaetigt: liefert Inverter/Meter/Storage/Ohmpilot als Maps
// Index->{DT,Serial}. Der Daemon iteriert ueber diese IDs bei jedem Poll-
// Zyklus neu (siehe poller.go) - kein hartcodiertes "es gibt genau N
// Geraete", damit spaeter zusaetzlich angeschlossene Smartmeter/Speicher
// automatisch mit erfasst werden.
type DeviceRef struct {
	DT     int    `json:"DT"`
	Serial string `json:"Serial"`
}

type ActiveDeviceInfo struct {
	Inverter map[string]DeviceRef `json:"Inverter"`
	Meter    map[string]DeviceRef `json:"Meter"`
	Storage  map[string]DeviceRef `json:"Storage"`
	Ohmpilot map[string]DeviceRef `json:"Ohmpilot"`
}

func (c *Client) GetActiveDeviceInfo() (*ActiveDeviceInfo, error) {
	data, err := c.get("/solar_api/v1/GetActiveDeviceInfo.cgi", url.Values{"DeviceClass": {"System"}})
	if err != nil {
		return nil, err
	}
	var info ActiveDeviceInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("GetActiveDeviceInfo: %w", err)
	}
	return &info, nil
}

// valueUnit ist die {"Unit":"...","Value":...}-Form, wie sie
// GetInverterRealtimeData fuer Scope=Device liefert. Value ist ein Pointer,
// weil DAY_ENERGY/YEAR_ENERGY live bestaetigt "null" sein koennen (z.B.
// direkt nach Mitternacht, bevor der erste Tageswert existiert) - ein
// Pointer unterscheidet das zuverlaessig von einer echten 0.
type valueUnit struct {
	Value *float64 `json:"Value"`
}

func deref(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

type deviceStatus struct {
	ErrorCode     int    `json:"ErrorCode"`
	StatusCode    int    `json:"StatusCode"`
	InverterState string `json:"InverterState"`
}

// CommonInverterData (GetInverterRealtimeData.cgi?DataCollection=CommonInverterData).
// DayEnergy/YearEnergy bleiben Pointer (siehe valueUnit) - der Poller
// veroeffentlicht das zugehoerige Topic dann bewusst NICHT statt faelschlich
// "0" zu senden.
type CommonInverterData struct {
	DayEnergy     *float64
	YearEnergy    *float64
	TotalEnergy   float64
	FAC           float64
	IAC           float64
	IDC           float64
	PAC           float64
	SAC           float64
	UAC           float64
	UDC           float64
	ErrorCode     int
	StatusCode    int
	InverterState string
}

func (c *Client) GetCommonInverterData(deviceID string) (*CommonInverterData, error) {
	data, err := c.get("/solar_api/v1/GetInverterRealtimeData.cgi", url.Values{
		"Scope": {"Device"}, "DeviceId": {deviceID}, "DataCollection": {"CommonInverterData"},
	})
	if err != nil {
		return nil, err
	}
	var raw struct {
		DAY_ENERGY   valueUnit    `json:"DAY_ENERGY"`
		YEAR_ENERGY  valueUnit    `json:"YEAR_ENERGY"`
		TOTAL_ENERGY valueUnit    `json:"TOTAL_ENERGY"`
		FAC          valueUnit    `json:"FAC"`
		IAC          valueUnit    `json:"IAC"`
		IDC          valueUnit    `json:"IDC"`
		PAC          valueUnit    `json:"PAC"`
		SAC          valueUnit    `json:"SAC"`
		UAC          valueUnit    `json:"UAC"`
		UDC          valueUnit    `json:"UDC"`
		DeviceStatus deviceStatus `json:"DeviceStatus"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("GetCommonInverterData: %w", err)
	}
	return &CommonInverterData{
		DayEnergy:     raw.DAY_ENERGY.Value,
		YearEnergy:    raw.YEAR_ENERGY.Value,
		TotalEnergy:   deref(raw.TOTAL_ENERGY.Value),
		FAC:           deref(raw.FAC.Value),
		IAC:           deref(raw.IAC.Value),
		IDC:           deref(raw.IDC.Value),
		PAC:           deref(raw.PAC.Value),
		SAC:           deref(raw.SAC.Value),
		UAC:           deref(raw.UAC.Value),
		UDC:           deref(raw.UDC.Value),
		ErrorCode:     raw.DeviceStatus.ErrorCode,
		StatusCode:    raw.DeviceStatus.StatusCode,
		InverterState: raw.DeviceStatus.InverterState,
	}, nil
}

// ThreePInverterData (DataCollection=3PInverterData) - Pro-Phase-Werte des
// Wechselrichters. Manche (kleinere, einphasige) Fronius-Geraete unterstuetzen
// diesen Scope nicht - der Poller behandelt einen Fehler hier als "keine
// 3P-Daten fuer dieses Geraet" statt den Zyklus abzubrechen.
type ThreePInverterData struct {
	IAC_L1, IAC_L2, IAC_L3 float64
	UAC_L1, UAC_L2, UAC_L3 float64
}

func (c *Client) Get3PInverterData(deviceID string) (*ThreePInverterData, error) {
	data, err := c.get("/solar_api/v1/GetInverterRealtimeData.cgi", url.Values{
		"Scope": {"Device"}, "DeviceId": {deviceID}, "DataCollection": {"3PInverterData"},
	})
	if err != nil {
		return nil, err
	}
	var raw struct {
		IAC_L1 valueUnit `json:"IAC_L1"`
		IAC_L2 valueUnit `json:"IAC_L2"`
		IAC_L3 valueUnit `json:"IAC_L3"`
		UAC_L1 valueUnit `json:"UAC_L1"`
		UAC_L2 valueUnit `json:"UAC_L2"`
		UAC_L3 valueUnit `json:"UAC_L3"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("Get3PInverterData: %w", err)
	}
	return &ThreePInverterData{
		IAC_L1: deref(raw.IAC_L1.Value), IAC_L2: deref(raw.IAC_L2.Value), IAC_L3: deref(raw.IAC_L3.Value),
		UAC_L1: deref(raw.UAC_L1.Value), UAC_L2: deref(raw.UAC_L2.Value), UAC_L3: deref(raw.UAC_L3.Value),
	}, nil
}

// StorageController (GetStorageRealtimeData.cgi?Scope=System) - Batterie-
// Zustand (Reserva). "Modules" (Zell-Detail-Array) ist beim echten Geraet
// leer ([]) - wird bewusst nicht abgebildet, da keine echten Daten drin
// stecken.
type StorageController struct {
	CapacityMaximum   float64
	DesignedCapacity  float64
	CurrentDC         float64
	VoltageDC         float64
	StateOfChargeRel  float64
	StatusBatteryCell float64
	TemperatureCell   float64
	Enable            int
	Manufacturer      string
	Model             string
	Serial            string
}

func (c *Client) GetStorageRealtimeData() (map[string]StorageController, error) {
	data, err := c.get("/solar_api/v1/GetStorageRealtimeData.cgi", url.Values{"Scope": {"System"}})
	if err != nil {
		return nil, err
	}
	var raw map[string]struct {
		Controller struct {
			CapacityMaximum       float64 `json:"Capacity_Maximum"`
			DesignedCapacity      float64 `json:"DesignedCapacity"`
			CurrentDC             float64 `json:"Current_DC"`
			VoltageDC             float64 `json:"Voltage_DC"`
			StateOfChargeRelative float64 `json:"StateOfCharge_Relative"`
			StatusBatteryCell     float64 `json:"Status_BatteryCell"`
			TemperatureCell       float64 `json:"Temperature_Cell"`
			Enable                int     `json:"Enable"`
			Details               struct {
				Manufacturer string `json:"Manufacturer"`
				Model        string `json:"Model"`
				Serial       string `json:"Serial"`
			} `json:"Details"`
		} `json:"Controller"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("GetStorageRealtimeData: %w", err)
	}
	out := make(map[string]StorageController, len(raw))
	for id, v := range raw {
		out[id] = StorageController{
			CapacityMaximum:   v.Controller.CapacityMaximum,
			DesignedCapacity:  v.Controller.DesignedCapacity,
			CurrentDC:         v.Controller.CurrentDC,
			VoltageDC:         v.Controller.VoltageDC,
			StateOfChargeRel:  v.Controller.StateOfChargeRelative,
			StatusBatteryCell: v.Controller.StatusBatteryCell,
			TemperatureCell:   v.Controller.TemperatureCell,
			Enable:            v.Controller.Enable,
			Manufacturer:      v.Controller.Details.Manufacturer,
			Model:             v.Controller.Details.Model,
			Serial:            v.Controller.Details.Serial,
		}
	}
	return out, nil
}

// MeterData (GetMeterRealtimeData.cgi?Scope=System) - ein Eintrag pro
// Smartmeter (beim Nutzer: Netzeinspeisepunkt + Untermessung "Werkstatt").
type MeterData struct {
	CurrentL1, CurrentL2, CurrentL3, CurrentSum         float64
	VoltageL1, VoltageL2, VoltageL3                     float64
	PowerRealL1, PowerRealL2, PowerRealL3, PowerRealSum float64
	PowerApparentSum                                    float64
	PowerFactorSum                                      float64
	EnergyConsumed, EnergyProduced                      float64
	FrequencyAverage                                    float64
	MeterLocation                                       float64
	Enable                                              int
	Manufacturer, Model, Serial                         string
}

func (c *Client) GetMeterRealtimeData() (map[string]MeterData, error) {
	data, err := c.get("/solar_api/v1/GetMeterRealtimeData.cgi", url.Values{"Scope": {"System"}})
	if err != nil {
		return nil, err
	}
	var raw map[string]struct {
		CurrentPhase1        float64 `json:"Current_AC_Phase_1"`
		CurrentPhase2        float64 `json:"Current_AC_Phase_2"`
		CurrentPhase3        float64 `json:"Current_AC_Phase_3"`
		CurrentSum           float64 `json:"Current_AC_Sum"`
		VoltagePhase1        float64 `json:"Voltage_AC_Phase_1"`
		VoltagePhase2        float64 `json:"Voltage_AC_Phase_2"`
		VoltagePhase3        float64 `json:"Voltage_AC_Phase_3"`
		PowerRealPhase1      float64 `json:"PowerReal_P_Phase_1"`
		PowerRealPhase2      float64 `json:"PowerReal_P_Phase_2"`
		PowerRealPhase3      float64 `json:"PowerReal_P_Phase_3"`
		PowerRealSum         float64 `json:"PowerReal_P_Sum"`
		PowerApparentSum     float64 `json:"PowerApparent_S_Sum"`
		PowerFactorSum       float64 `json:"PowerFactor_Sum"`
		EnergyConsumed       float64 `json:"EnergyReal_WAC_Sum_Consumed"`
		EnergyProduced       float64 `json:"EnergyReal_WAC_Sum_Produced"`
		FrequencyPhaseAvg    float64 `json:"Frequency_Phase_Average"`
		MeterLocationCurrent float64 `json:"Meter_Location_Current"`
		Enable               int     `json:"Enable"`
		Details              struct {
			Manufacturer string `json:"Manufacturer"`
			Model        string `json:"Model"`
			Serial       string `json:"Serial"`
		} `json:"Details"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("GetMeterRealtimeData: %w", err)
	}
	out := make(map[string]MeterData, len(raw))
	for id, v := range raw {
		out[id] = MeterData{
			CurrentL1: v.CurrentPhase1, CurrentL2: v.CurrentPhase2, CurrentL3: v.CurrentPhase3, CurrentSum: v.CurrentSum,
			VoltageL1: v.VoltagePhase1, VoltageL2: v.VoltagePhase2, VoltageL3: v.VoltagePhase3,
			PowerRealL1: v.PowerRealPhase1, PowerRealL2: v.PowerRealPhase2, PowerRealL3: v.PowerRealPhase3, PowerRealSum: v.PowerRealSum,
			PowerApparentSum: v.PowerApparentSum,
			PowerFactorSum:   v.PowerFactorSum,
			EnergyConsumed:   v.EnergyConsumed, EnergyProduced: v.EnergyProduced,
			FrequencyAverage: v.FrequencyPhaseAvg,
			MeterLocation:    v.MeterLocationCurrent,
			Enable:           v.Enable,
			Manufacturer:     v.Details.Manufacturer, Model: v.Details.Model, Serial: v.Details.Serial,
		}
	}
	return out, nil
}

// SiteData/InverterFlow (GetPowerFlowRealtimeData.fcgi) - Gesamtanlagen-Sicht
// inkl. SOC. P_Akku/P_PV/rel_Autonomy/rel_SelfConsumption bleiben Pointer:
// live bestaetigt anwesend, aber laut Fronius-Doku situativ null (z.B. keine
// PV-Erzeugung/kein Speicher erkannt).
type SiteData struct {
	PGrid              float64
	PLoad              float64
	PAkku              *float64
	PPV                *float64
	RelAutonomy        *float64
	RelSelfConsumption *float64
	BatteryStandby     bool
	BackupMode         bool
	Mode               string
	MeterLocation      string
}

type InverterFlow struct {
	P           float64
	SOC         *float64
	BatteryMode string
}

type PowerFlow struct {
	Site      SiteData
	Inverters map[string]InverterFlow
}

func (c *Client) GetPowerFlowRealtimeData() (*PowerFlow, error) {
	data, err := c.get("/solar_api/v1/GetPowerFlowRealtimeData.fcgi", nil)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Site struct {
			PGrid              float64  `json:"P_Grid"`
			PLoad              float64  `json:"P_Load"`
			PAkku              *float64 `json:"P_Akku"`
			PPV                *float64 `json:"P_PV"`
			RelAutonomy        *float64 `json:"rel_Autonomy"`
			RelSelfConsumption *float64 `json:"rel_SelfConsumption"`
			BatteryStandby     bool     `json:"BatteryStandby"`
			BackupMode         bool     `json:"BackupMode"`
			Mode               string   `json:"Mode"`
			MeterLocation      string   `json:"Meter_Location"`
		} `json:"Site"`
		Inverters map[string]struct {
			P           float64  `json:"P"`
			SOC         *float64 `json:"SOC"`
			BatteryMode string   `json:"Battery_Mode"`
		} `json:"Inverters"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("GetPowerFlowRealtimeData: %w", err)
	}
	pf := &PowerFlow{
		Site: SiteData{
			PGrid: raw.Site.PGrid, PLoad: raw.Site.PLoad,
			PAkku: raw.Site.PAkku, PPV: raw.Site.PPV,
			RelAutonomy: raw.Site.RelAutonomy, RelSelfConsumption: raw.Site.RelSelfConsumption,
			BatteryStandby: raw.Site.BatteryStandby, BackupMode: raw.Site.BackupMode,
			Mode: raw.Site.Mode, MeterLocation: raw.Site.MeterLocation,
		},
		Inverters: make(map[string]InverterFlow, len(raw.Inverters)),
	}
	for id, v := range raw.Inverters {
		pf.Inverters[id] = InverterFlow{P: v.P, SOC: v.SOC, BatteryMode: v.BatteryMode}
	}
	return pf, nil
}
