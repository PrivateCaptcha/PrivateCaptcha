package difficulty

import (
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
)

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
	return max(dbp.property.Level.Int16, int16(common.DifficultyLevelSmall-common.DifficultyDelta))
}

func (dbp *dbProperty) Growth() dbgen.DifficultyGrowth {
	return dbp.property.Growth
}
