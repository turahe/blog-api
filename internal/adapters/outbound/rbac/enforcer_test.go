package rbac

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/casbin/casbin/v3"
	"github.com/casbin/casbin/v3/model"
	"github.com/casbin/casbin/v3/persist"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// memAdapter is an in-memory casbin adapter holding policy lines such as "p, admin, *".
type memAdapter struct {
	mu      sync.Mutex
	lines   []string
	added   [][]string
	saved   []string
	loads   int
	loadErr error
	saveErr error
	addErr  error
	loaded  chan struct{}
}

func (a *memAdapter) LoadPolicy(m model.Model) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.loads++

	if a.loaded != nil {
		a.loaded <- struct{}{}
	}

	if a.loadErr != nil {
		return a.loadErr
	}

	for _, line := range a.lines {
		if err := persist.LoadPolicyLine(line, m); err != nil {
			return err
		}
	}

	return nil
}

func (a *memAdapter) SavePolicy(m model.Model) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.saveErr != nil {
		return a.saveErr
	}

	a.saved = nil

	for _, sec := range []string{"p", "g"} {
		for ptype, ast := range m[sec] {
			for _, rule := range ast.Policy {
				a.saved = append(a.saved, ptype+", "+strings.Join(rule, ", "))
			}
		}
	}

	slices.Sort(a.saved)

	return nil
}

func (a *memAdapter) AddPolicy(_, ptype string, rule []string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.addErr != nil {
		return a.addErr
	}

	a.added = append(a.added, append([]string{ptype}, rule...))

	return nil
}

func (a *memAdapter) RemovePolicy(string, string, []string) error { return nil }

func (a *memAdapter) RemoveFilteredPolicy(string, string, int, ...string) error { return nil }

func (a *memAdapter) setLines(lines ...string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.lines = lines
}

func (a *memAdapter) setLoadErr(err error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.loadErr = err
}

func (a *memAdapter) loadCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.loads
}

func newMemEnforcer(t *testing.T, a *memAdapter) *Enforcer {
	t.Helper()

	m, err := model.NewModelFromString(modelConf)
	require.NoError(t, err)

	e, err := casbin.NewSyncedEnforcer(m, a)
	require.NoError(t, err)

	return &Enforcer{e: e}
}

func TestRuleRowTableName(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "casbin_rules", ruleRow{}.TableName())
}

func TestPolicyRow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		ptype string
		rule  []string
		want  *ruleRow
	}{
		{name: "empty rule", ptype: "p", rule: nil, want: &ruleRow{Ptype: "p"}},
		{name: "permission rule", ptype: "p", rule: []string{"editor", "posts.write"}, want: &ruleRow{Ptype: "p", V0: "editor", V1: "posts.write"}},
		{name: "grouping rule", ptype: "g", rule: []string{"user-1", "editor"}, want: &ruleRow{Ptype: "g", V0: "user-1", V1: "editor"}},
		{
			name:  "six values fill every column",
			ptype: "p",
			rule:  []string{"a", "b", "c", "d", "e", "f"},
			want:  &ruleRow{Ptype: "p", V0: "a", V1: "b", V2: "c", V3: "d", V4: "e", V5: "f"},
		},
		{
			name:  "extra values are dropped",
			ptype: "p",
			rule:  []string{"a", "b", "c", "d", "e", "f", "g"},
			want:  &ruleRow{Ptype: "p", V0: "a", V1: "b", V2: "c", V3: "d", V4: "e", V5: "f"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, policyRow(tt.ptype, tt.rule))
		})
	}
}

func TestEnforcerEnforce(t *testing.T) {
	t.Parallel()

	editor, admin, direct, stranger := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	e := newMemEnforcer(t, &memAdapter{lines: []string{
		"p, editor, posts.write",
		"p, admin, *",
		"p, " + direct.String() + ", comments.moderate",
		"g, " + editor.String() + ", editor",
		"g, " + admin.String() + ", admin",
	}})

	tests := []struct {
		name       string
		user       uuid.UUID
		permission string
		want       bool
	}{
		{name: "role grants permission", user: editor, permission: "posts.write", want: true},
		{name: "role lacks other permission", user: editor, permission: "posts.delete", want: false},
		{name: "literal star is not a wildcard request", user: editor, permission: "*", want: false},
		{name: "wildcard grants any permission", user: admin, permission: "settings.write", want: true},
		{name: "wildcard grants star", user: admin, permission: "*", want: true},
		{name: "direct user permission", user: direct, permission: "comments.moderate", want: true},
		{name: "direct user limited to its permission", user: direct, permission: "posts.write", want: false},
		{name: "user without roles denied", user: stranger, permission: "posts.write", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := e.Enforce(context.Background(), tt.user, tt.permission)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestEnforcerAddRoleForUser(t *testing.T) {
	t.Parallel()

	errWrite := errors.New("write failed")

	tests := []struct {
		name      string
		adapter   *memAdapter
		wantErr   error
		wantAdded [][]string
		wantGrant bool
	}{
		{
			name:      "grants role and auto-saves rule",
			adapter:   &memAdapter{lines: []string{"p, editor, posts.write"}},
			wantAdded: [][]string{{"g", "USER", "editor"}},
			wantGrant: true,
		},
		{
			name:    "adapter failure is returned",
			adapter: &memAdapter{lines: []string{"p, editor, posts.write"}, addErr: errWrite},
			wantErr: errWrite,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			user := uuid.New()
			e := newMemEnforcer(t, tt.adapter)

			err := e.AddRoleForUser(context.Background(), user, "editor")
			require.ErrorIs(t, err, tt.wantErr)

			for _, rule := range tt.wantAdded {
				rule[1] = user.String()
			}

			assert.Equal(t, tt.wantAdded, tt.adapter.added)

			granted, err := e.Enforce(context.Background(), user, "posts.write")
			require.NoError(t, err)
			assert.Equal(t, tt.wantGrant, granted)
		})
	}
}

func TestEnforcerAddPermissionForRole(t *testing.T) {
	t.Parallel()

	user := uuid.New()
	e := newMemEnforcer(t, &memAdapter{lines: []string{"g, " + user.String() + ", editor"}})

	before, err := e.Enforce(context.Background(), user, "tags.write")
	require.NoError(t, err)
	require.False(t, before)

	require.NoError(t, e.AddPermissionForRole(context.Background(), "editor", "tags.write"))
	require.NoError(t, e.AddPermissionForRole(context.Background(), "editor", "tags.write"), "duplicate grant is not an error")

	after, err := e.Enforce(context.Background(), user, "tags.write")
	require.NoError(t, err)
	assert.True(t, after)
}

func TestEnforcerSave(t *testing.T) {
	t.Parallel()

	errSave := errors.New("save failed")

	tests := []struct {
		name      string
		saveErr   error
		wantSaved []string
	}{
		{name: "writes the whole policy", wantSaved: []string{"g, user-1, editor", "p, admin, *", "p, editor, posts.write"}},
		{name: "adapter failure is returned", saveErr: errSave},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			adapter := &memAdapter{lines: []string{"p, editor, posts.write", "g, user-1, editor"}, saveErr: tt.saveErr}
			e := newMemEnforcer(t, adapter)
			require.NoError(t, e.AddPermissionForRole(context.Background(), "admin", "*"))

			require.ErrorIs(t, e.Save(), tt.saveErr)
			assert.Equal(t, tt.wantSaved, adapter.saved)
		})
	}
}

func TestEnforcerReload(t *testing.T) {
	t.Parallel()

	user := uuid.New()
	adapter := &memAdapter{lines: []string{"p, editor, posts.write"}}
	e := newMemEnforcer(t, adapter)

	granted, err := e.Enforce(context.Background(), user, "posts.write")
	require.NoError(t, err)
	require.False(t, granted)

	adapter.setLines("p, editor, posts.write", "g, "+user.String()+", editor")
	require.NoError(t, e.Reload())

	granted, err = e.Enforce(context.Background(), user, "posts.write")
	require.NoError(t, err)
	assert.True(t, granted, "reload picks up rows written elsewhere")

	errLoad := errors.New("load failed")
	adapter.setLoadErr(errLoad)
	require.ErrorIs(t, e.Reload(), errLoad)
}
