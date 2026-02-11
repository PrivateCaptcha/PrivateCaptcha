package difficulty

import dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"

type Property interface {
	ID() int32
	Valid() bool
	OwnerID() int32
	OrgID() int32
	Level() int16
	Growth() dbgen.DifficultyGrowth
}

func NewDBProperty(p *dbgen.Property) *dbProperty {
	return &dbProperty{property: p}
}

type dbProperty struct {
	property *dbgen.Property
}

func (dbp *dbProperty) Valid() bool {
	return dbp.property != nil && dbp.property.ExternalID.Valid
}

func (dbp *dbProperty) ID() int32 {
	return dbp.property.ID
}

func (dbp *dbProperty) OwnerID() int32 {
	// we record events for the user that owns the org where the property belongs
	// (effectively, who is billed for the org), rather than who created it
	return dbp.property.OrgOwnerID.Int32
}

func (dbp *dbProperty) OrgID() int32 {
	return dbp.property.OrgID.Int32
}

func (dbp *dbProperty) Level() int16 {
	return dbp.property.Level.Int16
}

func (dbp *dbProperty) Growth() dbgen.DifficultyGrowth {
	return dbp.property.Growth
}

// StubProperty is a test helper that implements the Property interface
type StubProperty struct {
	id      int32
	valid   bool
	ownerID int32
	orgID   int32
	level   int16
	growth  dbgen.DifficultyGrowth
}

func NewStubProperty(id int32, valid bool, ownerID, orgID int32, level int16, growth dbgen.DifficultyGrowth) *StubProperty {
	return &StubProperty{id: id, valid: valid, ownerID: ownerID, orgID: orgID, level: level, growth: growth}
}

func (p *StubProperty) ID() int32                      { return p.id }
func (p *StubProperty) Valid() bool                    { return p.valid }
func (p *StubProperty) OwnerID() int32                 { return p.ownerID }
func (p *StubProperty) OrgID() int32                   { return p.orgID }
func (p *StubProperty) Level() int16                   { return p.level }
func (p *StubProperty) Growth() dbgen.DifficultyGrowth { return p.growth }
