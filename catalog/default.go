package catalog

// DefaultSnapshotID is the well-known identifier of the built-in rule snapshot.
const DefaultSnapshotID = "rules-east-1"

// DefaultCatalog returns a catalog seeded with a single built-in snapshot for
// the "east" harvest area. It is the seed used by the server so the lock flow
// has a trusted snapshot without any external configuration.
func DefaultCatalog() *Catalog {
	c := NewCatalog()
	c.Register(DefaultSnapshot())
	return c
}

// DefaultSnapshot returns the built-in snapshot for the east area.
func DefaultSnapshot() RuleSnapshot {
	scales := DefaultScales()
	thresholds := Thresholds{
		MaxMortalityPermille: 100,  // 10% mortality
		MaxPSP:               800,  // 0.800 scaled by 1e3
		MaxDSP:               160,  // 0.160 scaled by 1e3
		MaxNorovirusCt:       350,  // 35.0 scaled by 1e1
		MaxColiform:          100,  // integer
		MinSalinity:          2500, // 25.00
		MaxSalinity:          3800, // 38.00
		MaxTemperature:       2800, // 28.00
		MaxChlorine:          50,   // 0.50
		MaxTurbidity:         50,   // 5.0
	}
	return RuleSnapshot{
		ID:         DefaultSnapshotID,
		Version:    1,
		AreaID:     "east",
		Tides:      []TidePattern{"T1", "T2"},
		Scales:     scales,
		Thresholds: thresholds,
		Digest:     Digest("east", []TidePattern{"T1", "T2"}, scales, thresholds),
	}
}
