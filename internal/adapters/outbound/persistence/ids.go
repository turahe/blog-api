package persistence

import (
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// errUnknownReference means a public uuid did not match any row in the referenced table.
var errUnknownReference = errors.New("unknown reference")

// idByUUID resolves a public uuid to the bigint primary key of table.
func idByUUID(db *gorm.DB, table string, id uuid.UUID) (int64, error) {
	var ids []int64
	if err := db.Table(table).Where("uuid = ?", id).Limit(1).Pluck("id", &ids).Error; err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, fmt.Errorf("%s %s: %w", table, id, errUnknownReference)
	}
	return ids[0], nil
}

// optionalIDByUUID is idByUUID for nullable references; nil maps to nil.
func optionalIDByUUID(db *gorm.DB, table string, id *uuid.UUID) (*int64, error) {
	if id == nil {
		return nil, nil
	}
	resolved, err := idByUUID(db, table, *id)
	if err != nil {
		return nil, err
	}
	return &resolved, nil
}

// uuidRef selects the uuid of the row in table referenced by fkColumn as alias.
// fkColumn must be qualified with the outer table; the inner table is aliased
// "ref" so self-references (parent_id, replaced_by) resolve correctly.
func uuidRef(table, fkColumn, alias string) string {
	return fmt.Sprintf("(SELECT ref.uuid FROM %s ref WHERE ref.id = %s) AS %s", table, fkColumn, alias)
}

// withRefs builds a SELECT list of table.* plus uuidRef expressions.
func withRefs(table string, refs ...string) string {
	return strings.Join(append([]string{table + ".*"}, refs...), ", ")
}

// idOf is the subquery form of idByUUID for WHERE clauses; bind the uuid as its argument.
func idOf(table string) string {
	return "(SELECT id FROM " + table + " WHERE uuid = ?)"
}
