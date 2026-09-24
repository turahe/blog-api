// Package rbac adapts Casbin, persisted in casbin_rules via GORM, to the RBAC port.
package rbac

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"github.com/casbin/casbin/v2/persist"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

//go:embed model.conf
var modelConf string

type ruleRow struct {
	ID    uint   `gorm:"primaryKey;autoIncrement"`
	Ptype string `gorm:"column:ptype;size:100"`
	V0    string `gorm:"column:v0;size:255"`
	V1    string `gorm:"column:v1;size:255"`
	V2    string `gorm:"column:v2;size:255"`
	V3    string `gorm:"column:v3;size:255"`
	V4    string `gorm:"column:v4;size:255"`
	V5    string `gorm:"column:v5;size:255"`
}

func (ruleRow) TableName() string { return "casbin_rules" }

type adapter struct {
	db *gorm.DB
}

func (a *adapter) LoadPolicy(m model.Model) error {
	var rows []ruleRow
	if err := a.db.Find(&rows).Error; err != nil {
		return err
	}

	for _, row := range rows {
		var line strings.Builder

		line.WriteString(row.Ptype)

		for _, v := range []string{row.V0, row.V1, row.V2, row.V3, row.V4, row.V5} {
			if v == "" {
				break
			}

			line.WriteString(", " + v)
		}

		if err := persist.LoadPolicyLine(line.String(), m); err != nil {
			return err
		}
	}

	return nil
}

func (a *adapter) SavePolicy(m model.Model) error {
	return a.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("1 = 1").Delete(&ruleRow{}).Error; err != nil {
			return err
		}

		for ptype, ast := range m["p"] {
			for _, rule := range ast.Policy {
				if err := tx.Create(policyRow(ptype, rule)).Error; err != nil {
					return err
				}
			}
		}

		for ptype, ast := range m["g"] {
			for _, rule := range ast.Policy {
				if err := tx.Create(policyRow(ptype, rule)).Error; err != nil {
					return err
				}
			}
		}

		return nil
	})
}

func (a *adapter) AddPolicy(_, ptype string, rule []string) error {
	return a.db.Create(policyRow(ptype, rule)).Error
}

func (a *adapter) RemovePolicy(_, ptype string, rule []string) error {
	q := a.db.Where("ptype = ?", ptype)

	vals := []string{"v0", "v1", "v2", "v3", "v4", "v5"}
	for i, v := range rule {
		if i >= len(vals) {
			break
		}

		q = q.Where(vals[i]+" = ?", v)
	}

	return q.Delete(&ruleRow{}).Error
}

func (a *adapter) RemoveFilteredPolicy(_, ptype string, fieldIndex int, fieldValues ...string) error {
	q := a.db.Where("ptype = ?", ptype)
	vals := []string{"v0", "v1", "v2", "v3", "v4", "v5"}

	for i, v := range fieldValues {
		idx := fieldIndex + i
		if idx < 0 || idx >= len(vals) || v == "" {
			continue
		}

		q = q.Where(vals[idx]+" = ?", v)
	}

	return q.Delete(&ruleRow{}).Error
}

func policyRow(ptype string, rule []string) *ruleRow {
	row := &ruleRow{Ptype: ptype}

	fields := []*string{&row.V0, &row.V1, &row.V2, &row.V3, &row.V4, &row.V5}
	for i, v := range rule {
		if i >= len(fields) {
			break
		}

		*fields[i] = v
	}

	return row
}

// Enforcer implements rbacports.Enforcer on a Casbin RBAC model. The synced
// enforcer makes runtime policy writes and reloads safe alongside Enforce.
type Enforcer struct {
	e *casbin.SyncedEnforcer
}

// NewEnforcer loads the model and the persisted policy from casbin_rules.
func NewEnforcer(db *gorm.DB) (*Enforcer, error) {
	m, err := model.NewModelFromString(modelConf)
	if err != nil {
		return nil, fmt.Errorf("casbin model: %w", err)
	}

	enforcer, err := casbin.NewSyncedEnforcer(m, &adapter{db: db})
	if err != nil {
		return nil, fmt.Errorf("casbin enforcer: %w", err)
	}

	if err := enforcer.LoadPolicy(); err != nil {
		return nil, fmt.Errorf("load casbin policy: %w", err)
	}

	return &Enforcer{e: enforcer}, nil
}

// Enforce reports whether the user holds permission directly or through a role ("*" grants all).
func (e *Enforcer) Enforce(_ context.Context, userID uuid.UUID, permission string) (bool, error) {
	return e.e.Enforce(userID.String(), permission)
}

// AddRoleForUser grants role to the user. The adapter auto-saves the new rule;
// Save rewrites the whole table from memory.
func (e *Enforcer) AddRoleForUser(_ context.Context, userID uuid.UUID, role string) error {
	_, err := e.e.AddGroupingPolicy(userID.String(), role)
	return err
}

// AddPermissionForRole grants permission to role in the in-memory policy; call Save to persist.
func (e *Enforcer) AddPermissionForRole(_ context.Context, role, permission string) error {
	_, err := e.e.AddPolicy(role, permission)
	return err
}

// Save persists the in-memory policy to casbin_rules.
func (e *Enforcer) Save() error {
	return e.e.SavePolicy()
}
