package bridge

import "fmt"

// reachability merkt, ob der letzte Poll-Zyklus den Wechselrichter erreicht
// hat, damit Fehler nur beim Zustandswechsel geloggt werden statt bei jedem
// Zyklus (ein ueber Nacht abgeschalteter/neu startender WR wuerde sonst
// alle paar Sekunden eine Zeile schreiben).
type reachability struct {
	known bool
	ok    bool
}

// update liefert die Log-Meldung und changed=true, wenn sich der Zustand
// geaendert hat (oder es der erste Zyklus mit Fehler ist).
func (r *reachability) update(err error) (msg string, changed bool) {
	ok := err == nil
	first := !r.known
	if !first && r.ok == ok {
		return "", false
	}
	r.known, r.ok = true, ok
	if ok {
		if first {
			return "", false // normaler Start - kein Extra-Log
		}
		return "fronius: Wechselrichter wieder erreichbar", true
	}
	return fmt.Sprintf("fronius: Poll-Zyklus abgebrochen, Wechselrichter nicht erreichbar: %v", err), true
}
