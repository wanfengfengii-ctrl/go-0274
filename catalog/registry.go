package catalog

// Catalog is an in-memory registry of rule snapshots keyed by identifier. The
// registry is populated at process startup and is the only source of trusted
// snapshots; a lock may only reference snapshots that exist here.
type Catalog struct {
	snapshots map[string]RuleSnapshot
}

// NewCatalog builds an empty catalog.
func NewCatalog() *Catalog {
	return &Catalog{snapshots: make(map[string]RuleSnapshot)}
}

// Register adds or replaces a snapshot under its identifier.
func (c *Catalog) Register(s RuleSnapshot) {
	c.snapshots[s.ID] = s
}

// Get returns the snapshot for an identifier.
func (c *Catalog) Get(id string) (RuleSnapshot, bool) {
	s, ok := c.snapshots[id]
	return s, ok
}

// Resolve returns the snapshot whose canonical digest matches the submitted
// digest. This is used by the lock flow: the caller supplies the snapshot id
// and digest, and Resolve confirms both are consistent with the registry.
func (c *Catalog) Resolve(id, digest string) (RuleSnapshot, bool) {
	s, ok := c.snapshots[id]
	if !ok {
		return RuleSnapshot{}, false
	}
	if s.Digest != digest {
		return RuleSnapshot{}, false
	}
	return s, true
}
