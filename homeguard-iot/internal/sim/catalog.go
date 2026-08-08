package sim

// Static catalog data: plans, regions, device types. These get upserted on
// simulator startup and rarely change. Keeping them in code (vs a seed file)
// means the demo is self-contained and reproducible — no external CSVs to
// keep in sync.

// Plan describes one subscription tier with its monitoring SLA.
type Plan struct {
	ID         int
	Name       string
	PriceUSD   float64
	SLASeconds int
}

var Plans = []Plan{
	{ID: 1, Name: "Basic", PriceUSD: 19.99, SLASeconds: 300},   // 5 min
	{ID: 2, Name: "Plus", PriceUSD: 39.99, SLASeconds: 120},    // 2 min
	{ID: 3, Name: "Premium", PriceUSD: 59.99, SLASeconds: 60},  // 1 min
	{ID: 4, Name: "Concierge", PriceUSD: 99.99, SLASeconds: 30}, // 30 s
}

// Region groups households for dispatch + timezone purposes.
type Region struct {
	ID             int
	Name           string
	DispatchCenter string
	Timezone       string
}

var Regions = []Region{
	{1, "Northeast", "Boston DC", "America/New_York"},
	{2, "Mid-Atlantic", "Newark DC", "America/New_York"},
	{3, "Southeast", "Atlanta DC", "America/New_York"},
	{4, "Midwest", "Chicago DC", "America/Chicago"},
	{5, "South-Central", "Dallas DC", "America/Chicago"},
	{6, "Mountain", "Denver DC", "America/Denver"},
	{7, "Pacific", "Phoenix DC", "America/Phoenix"},
	{8, "Pacific-NW", "Seattle DC", "America/Los_Angeles"},
	{9, "California", "San Jose DC", "America/Los_Angeles"},
	{10, "Canada-East", "Toronto DC", "America/Toronto"},
}

// DeviceType is the sensor catalog. `DefaultSeverity` is the baseline
// alert severity if a device of this type fires a triggered event with the
// household armed — alert rules can promote or demote per-event.
type DeviceType struct {
	ID              int
	Code            string
	Name            string
	DefaultSeverity int
}

var DeviceTypes = []DeviceType{
	{ID: 1, Code: "MOTION", Name: "Motion sensor", DefaultSeverity: 3},
	{ID: 2, Code: "DOOR", Name: "Door contact", DefaultSeverity: 4},
	{ID: 3, Code: "WINDOW", Name: "Window contact", DefaultSeverity: 4},
	{ID: 4, Code: "GLASS_BREAK", Name: "Glass-break detector", DefaultSeverity: 4},
	{ID: 5, Code: "SMOKE", Name: "Smoke detector", DefaultSeverity: 5},
	{ID: 6, Code: "CO", Name: "Carbon monoxide detector", DefaultSeverity: 5},
	{ID: 7, Code: "WATER", Name: "Water leak sensor", DefaultSeverity: 3},
	{ID: 8, Code: "TEMP", Name: "Temperature sensor", DefaultSeverity: 1},
	{ID: 9, Code: "DOORBELL", Name: "Smart doorbell", DefaultSeverity: 1},
	{ID: 10, Code: "KEYPAD", Name: "Entry keypad", DefaultSeverity: 1},
}

// Locations is the pool of in-home placements used when allocating devices.
// Not every device type lives in every location, but the simulator picks
// per-type plausibly (see allocateDevices in simulator.go).
var Locations = []string{
	"front_door", "back_door", "garage_door", "side_door",
	"living_room", "kitchen", "dining_room",
	"master_bedroom", "bedroom_2", "bedroom_3",
	"basement", "attic", "utility_room", "hallway",
}
