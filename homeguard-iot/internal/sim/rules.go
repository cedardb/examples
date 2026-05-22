package sim

// Alert rules: deterministically promote certain triggered events into
// rows in the alerts table. The mapping is intentionally simple so the
// query that joins alerts × households × plans × device_types tells a
// readable story on the dashboard.
//
// kind values mirror the SMALLINT column in events:
//
//	0 = heartbeat       (never alerts)
//	1 = triggered       (the alerter; consult device_type + armed state)
//	2 = battery_low     (info-level alert if severity >= 4 plan SLA)
//	3 = offline         (skipped for now; could become an alert if device
//	                     was previously online for a long time)
//	4 = tamper          (always alerts at severity 4)

// AlertDecision says whether to raise an alert for a given event, and at
// what severity. nil = don't raise.
type AlertDecision struct {
	Severity int
	Detail   string
}

// EvaluateAlert applies the demo's alert rules to a candidate event.
// Inputs are the device type code (e.g. "SMOKE"), event kind, and whether
// the household was armed at the moment the event fired.
func EvaluateAlert(deviceCode string, kind int, armed bool) *AlertDecision {
	switch kind {
	case 4: // tamper — always alert, anywhere
		return &AlertDecision{Severity: 4, Detail: "Device tamper detected"}
	case 1: // triggered — depends on device type and armed state
		switch deviceCode {
		case "SMOKE":
			return &AlertDecision{Severity: 5, Detail: "Smoke detected"}
		case "CO":
			return &AlertDecision{Severity: 5, Detail: "Carbon monoxide detected"}
		case "GLASS_BREAK":
			return &AlertDecision{Severity: 4, Detail: "Glass break detected"}
		case "WATER":
			return &AlertDecision{Severity: 3, Detail: "Water leak detected"}
		case "DOOR", "WINDOW":
			if armed {
				return &AlertDecision{Severity: 4, Detail: deviceCode + " opened while armed"}
			}
			// Door/window events with the system disarmed are routine.
			return nil
		case "MOTION":
			if armed {
				return &AlertDecision{Severity: 3, Detail: "Motion detected while armed"}
			}
			return nil
		case "DOORBELL":
			// Informational only.
			return nil
		default:
			return nil
		}
	default:
		return nil
	}
}
