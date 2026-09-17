package fronius

import (
	"log"
	"strconv"
)

// KV ist ein einzelner MQTT-State-Topic/Wert-Fund eines Poll-Zyklus - die
// Bridge (siehe internal/bridge) veroeffentlicht jeden Eintrag retained unter
// <prefix><Topic>.
type KV struct {
	Topic string
	Value string
}

type Poller struct {
	client *Client
}

func NewPoller(client *Client) *Poller {
	return &Poller{client: client}
}

// Poll fuehrt einen vollstaendigen Abfragezyklus durch: Discovery
// (GetActiveDeviceInfo, siehe client.go) gefolgt von den Detail-Endpunkten
// fuer jedes gefundene Geraet. Die Discovery laeuft bei JEDEM Zyklus neu
// (nicht nur beim Start) - damit ein spaeter zusaetzlich angeschlossenes
// Geraet automatisch ohne Neustart des Diensts erscheint. Ein fehlschlagender
// Teil-Abruf (z.B. 3P-Daten auf einem Geraet ohne diesen Scope) bricht den
// gesamten Zyklus NICHT ab, nur diese Werte fehlen dann fuer diesen Zyklus.
func (p *Poller) Poll() []KV {
	var kv []KV

	info, err := p.client.GetActiveDeviceInfo()
	if err != nil {
		log.Printf("fronius: GetActiveDeviceInfo fehlgeschlagen: %v", err)
		return kv
	}

	for id := range info.Inverter {
		base := "inverter/" + id + "/state/"

		if common, err := p.client.GetCommonInverterData(id); err != nil {
			log.Printf("fronius: CommonInverterData(%s) fehlgeschlagen: %v", id, err)
		} else {
			addFloatPtr(&kv, base+"day_energy", common.DayEnergy)
			addFloatPtr(&kv, base+"year_energy", common.YearEnergy)
			addFloat(&kv, base+"total_energy", common.TotalEnergy)
			addFloat(&kv, base+"pac", common.PAC)
			addFloat(&kv, base+"sac", common.SAC)
			addFloat(&kv, base+"iac", common.IAC)
			addFloat(&kv, base+"uac", common.UAC)
			addFloat(&kv, base+"fac", common.FAC)
			addFloat(&kv, base+"idc", common.IDC)
			addFloat(&kv, base+"udc", common.UDC)
			addInt(&kv, base+"error_code", common.ErrorCode)
			addInt(&kv, base+"status_code", common.StatusCode)
			addString(&kv, base+"inverter_state", common.InverterState)
		}

		if threeP, err := p.client.Get3PInverterData(id); err != nil {
			// Kein lautes Fehler-Log auf Info-Ebene noetig: manche
			// (einphasige) Geraete unterstuetzen diesen Scope schlicht
			// nicht - erwartbarer Regelfall, kein Betriebsproblem.
			log.Printf("fronius: 3PInverterData(%s) nicht verfuegbar: %v", id, err)
		} else {
			addFloat(&kv, base+"uac_l1", threeP.UAC_L1)
			addFloat(&kv, base+"uac_l2", threeP.UAC_L2)
			addFloat(&kv, base+"uac_l3", threeP.UAC_L3)
			addFloat(&kv, base+"iac_l1", threeP.IAC_L1)
			addFloat(&kv, base+"iac_l2", threeP.IAC_L2)
			addFloat(&kv, base+"iac_l3", threeP.IAC_L3)
		}
	}

	if len(info.Storage) > 0 {
		if storages, err := p.client.GetStorageRealtimeData(); err != nil {
			log.Printf("fronius: GetStorageRealtimeData fehlgeschlagen: %v", err)
		} else {
			for id, s := range storages {
				base := "storage/" + id + "/state/"
				addFloat(&kv, base+"soc", s.StateOfChargeRel)
				addFloat(&kv, base+"capacity_max", s.CapacityMaximum)
				addFloat(&kv, base+"designed_capacity", s.DesignedCapacity)
				addFloat(&kv, base+"current_dc", s.CurrentDC)
				addFloat(&kv, base+"voltage_dc", s.VoltageDC)
				addFloat(&kv, base+"temperature", s.TemperatureCell)
				addFloat(&kv, base+"status_battery_cell", s.StatusBatteryCell)
				addInt(&kv, base+"enable", s.Enable)
				addString(&kv, base+"manufacturer", s.Manufacturer)
				addString(&kv, base+"model", s.Model)
			}
		}
	}

	if len(info.Meter) > 0 {
		if meters, err := p.client.GetMeterRealtimeData(); err != nil {
			log.Printf("fronius: GetMeterRealtimeData fehlgeschlagen: %v", err)
		} else {
			for id, m := range meters {
				base := "meter/" + id + "/state/"
				addFloat(&kv, base+"power_sum", m.PowerRealSum)
				addFloat(&kv, base+"power_l1", m.PowerRealL1)
				addFloat(&kv, base+"power_l2", m.PowerRealL2)
				addFloat(&kv, base+"power_l3", m.PowerRealL3)
				addFloat(&kv, base+"power_apparent_sum", m.PowerApparentSum)
				addFloat(&kv, base+"power_factor_sum", m.PowerFactorSum)
				addFloat(&kv, base+"voltage_l1", m.VoltageL1)
				addFloat(&kv, base+"voltage_l2", m.VoltageL2)
				addFloat(&kv, base+"voltage_l3", m.VoltageL3)
				addFloat(&kv, base+"current_l1", m.CurrentL1)
				addFloat(&kv, base+"current_l2", m.CurrentL2)
				addFloat(&kv, base+"current_l3", m.CurrentL3)
				addFloat(&kv, base+"current_sum", m.CurrentSum)
				addFloat(&kv, base+"energy_consumed", m.EnergyConsumed)
				addFloat(&kv, base+"energy_produced", m.EnergyProduced)
				addFloat(&kv, base+"frequency", m.FrequencyAverage)
				addFloat(&kv, base+"meter_location", m.MeterLocation)
				addString(&kv, base+"manufacturer", m.Manufacturer)
				addString(&kv, base+"model", m.Model)
			}
		}
	}

	if pf, err := p.client.GetPowerFlowRealtimeData(); err != nil {
		log.Printf("fronius: GetPowerFlowRealtimeData fehlgeschlagen: %v", err)
	} else {
		addFloat(&kv, "site/state/p_grid", pf.Site.PGrid)
		addFloat(&kv, "site/state/p_load", pf.Site.PLoad)
		addFloatPtr(&kv, "site/state/p_akku", pf.Site.PAkku)
		addFloatPtr(&kv, "site/state/p_pv", pf.Site.PPV)
		addFloatPtr(&kv, "site/state/rel_autonomy", pf.Site.RelAutonomy)
		addFloatPtr(&kv, "site/state/rel_selfconsumption", pf.Site.RelSelfConsumption)
		addBool(&kv, "site/state/battery_standby", pf.Site.BatteryStandby)
		addBool(&kv, "site/state/backup_mode", pf.Site.BackupMode)
		addString(&kv, "site/state/mode", pf.Site.Mode)
		for id, inv := range pf.Inverters {
			base := "inverter/" + id + "/state/"
			addFloatPtr(&kv, base+"soc", inv.SOC)
			addString(&kv, base+"battery_mode", inv.BatteryMode)
		}
	}

	return kv
}

func addFloat(kv *[]KV, topic string, v float64) {
	*kv = append(*kv, KV{Topic: topic, Value: strconv.FormatFloat(v, 'f', 3, 64)})
}

// addFloatPtr veroeffentlicht das Topic bewusst GAR NICHT, wenn v nil ist
// (JSON "null" - z.B. DAY_ENERGY vor dem ersten Tageswert) - eine falsche
// "0" waere hier schlimmer als ein zeitweise fehlendes Topic (retained MQTT
// behaelt ohnehin den letzten bekannten Wert).
func addFloatPtr(kv *[]KV, topic string, v *float64) {
	if v == nil {
		return
	}
	addFloat(kv, topic, *v)
}

func addInt(kv *[]KV, topic string, v int) {
	*kv = append(*kv, KV{Topic: topic, Value: strconv.Itoa(v)})
}

func addBool(kv *[]KV, topic string, v bool) {
	*kv = append(*kv, KV{Topic: topic, Value: strconv.FormatBool(v)})
}

func addString(kv *[]KV, topic string, v string) {
	*kv = append(*kv, KV{Topic: topic, Value: v})
}
