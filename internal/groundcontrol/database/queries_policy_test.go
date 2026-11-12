package database_test

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"sync"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func requirePQCode(t *testing.T, err error, code string) {
	t.Helper()
	var pqErr *pq.Error
	require.True(t, errors.As(err, &pqErr), "want SQLSTATE %s, got %v", code, err)
	require.Equal(t, code, string(pqErr.Code), pqErr.Message)
}

func TestPolicyRules_SQLContract(t *testing.T) {
	db := dbtest.New(t)
	q := database.New(db.DB)
	ctx := context.Background()
	groupAdmin := database.InsertPolicyRuleParams{Subject: "role:group-admin:A", Scope: "group:A", ResourceType: "group", Action: "*", Effect: "allow"}

	rule, err := q.InsertPolicyRule(ctx, groupAdmin)
	require.NoError(t, err)
	require.Positive(t, rule.ID)
	require.Equal(t, database.InsertPolicyRuleRow{ID: rule.ID, Subject: "role:group-admin:A", Scope: "group:A", ResourceType: "group", Action: "*", Effect: "allow"}, rule)
	_, err = q.InsertPolicyRule(ctx, groupAdmin)
	requirePQCode(t, err, "23505")
	deny := groupAdmin
	deny.Effect = "deny"
	denyRule, err := q.InsertPolicyRule(ctx, deny)
	require.NoError(t, err, "the same line with the other effect is another rule")
	maybe := groupAdmin
	maybe.Effect = "maybe"
	_, err = q.InsertPolicyRule(ctx, maybe)
	requirePQCode(t, err, "23514")

	rules, err := q.ListPolicyRules(ctx)
	require.NoError(t, err)
	require.Len(t, rules, 2)
	require.Equal(t, []int64{rule.ID, denyRule.ID}, []int64{rules[0].ID, rules[1].ID}, "ordered by id")

	n, err := q.DeletePolicyRule(ctx, rule.ID)
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	n, err = q.DeletePolicyRule(ctx, rule.ID)
	require.NoError(t, err)
	require.Zero(t, n, "a missing rule deletes nothing")

	binding, err := q.InsertRoleBinding(ctx, database.InsertRoleBindingParams{Principal: "user:bob", Role: "role:group-admin:A", CreatedBy: sql.NullString{String: "admin", Valid: true}})
	require.NoError(t, err)
	require.Equal(t, database.InsertRoleBindingRow{ID: binding.ID, Principal: "user:bob", Role: "role:group-admin:A"}, binding)
	_, err = q.InsertRoleBinding(ctx, database.InsertRoleBindingParams{Principal: "user:bob", Role: "role:group-admin:A"})
	requirePQCode(t, err, "23505")
	bindings, err := q.ListRoleBindings(ctx)
	require.NoError(t, err)
	require.Equal(t, []database.ListRoleBindingsRow{{ID: binding.ID, Principal: "user:bob", Role: "role:group-admin:A"}}, bindings)
	n, err = q.DeleteRoleBinding(ctx, binding.ID)
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	n, err = q.DeleteRoleBinding(ctx, binding.ID)
	require.NoError(t, err)
	require.Zero(t, n)
}

// Concurrent writers each bump once inside their own transaction; the row
// lock hands out every version exactly once.
func TestPolicyVersion_BumpIsMonotonic(t *testing.T) {
	db := dbtest.New(t)
	q := database.New(db.DB)
	ctx := context.Background()
	v, err := q.GetPolicyVersion(ctx)
	require.NoError(t, err)
	require.Zero(t, v, "migration 020 seeds version 0")

	const writers = 8
	versions := make([]int64, writers)
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for i := range writers {
		wg.Go(func() {
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				errs <- err
				return
			}
			got, err := q.WithTx(tx).BumpPolicyVersion(ctx)
			if err != nil {
				errs <- errors.Join(err, tx.Rollback())
				return
			}
			versions[i] = got
			errs <- tx.Commit()
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	sort.Slice(versions, func(a, b int) bool { return versions[a] < versions[b] })
	require.Equal(t, []int64{1, 2, 3, 4, 5, 6, 7, 8}, versions)
	v, err = q.GetPolicyVersion(ctx)
	require.NoError(t, err)
	require.EqualValues(t, writers, v)
}

// The policy snapshot reads version, rules and bindings in one REPEATABLE
// READ transaction, version first: a write committed after the version was
// read is invisible to the rest of the snapshot.
func TestPolicySnapshot_RepeatableReadIsConsistent(t *testing.T) {
	db := dbtest.New(t)
	q := database.New(db.DB)
	ctx := context.Background()

	reader, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	require.NoError(t, err)
	defer func() {
		if err := reader.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			t.Error(err)
		}
	}()
	version, err := q.WithTx(reader).GetPolicyVersion(ctx)
	require.NoError(t, err)

	writer, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = q.WithTx(writer).InsertPolicyRule(ctx, database.InsertPolicyRuleParams{Subject: "user:bob", Scope: "*", ResourceType: "group", Action: "read", Effect: "allow"})
	require.NoError(t, err)
	bumped, err := q.WithTx(writer).BumpPolicyVersion(ctx)
	require.NoError(t, err)
	require.NoError(t, writer.Commit())
	require.Equal(t, version+1, bumped)

	rules, err := q.WithTx(reader).ListPolicyRules(ctx)
	require.NoError(t, err)
	require.Empty(t, rules, "the snapshot of version %d does not see version %d's rule", version, bumped)
	require.NoError(t, reader.Commit())

	rules, err = q.ListPolicyRules(ctx)
	require.NoError(t, err)
	require.Len(t, rules, 1)
}

func TestListGroupNamesOfSatellites_SQLContract(t *testing.T) {
	db := dbtest.New(t)
	q := database.New(db.DB)
	ctx := context.Background()
	satellite := func(name string) int32 {
		t.Helper()
		s, err := q.CreateSatellite(ctx, name)
		require.NoError(t, err)
		return s.ID
	}
	group := func(name string) int32 {
		t.Helper()
		g, err := q.CreateGroup(ctx, database.CreateGroupParams{GroupName: name, RegistryUrl: "http://harbor.example", Projects: []string{"library"}})
		require.NoError(t, err)
		return g.ID
	}
	member := func(sat, grp int32) {
		t.Helper()
		require.NoError(t, q.AddSatelliteToGroup(ctx, database.AddSatelliteToGroupParams{SatelliteID: sat, GroupID: grp}))
	}
	s1, s2, s3 := satellite("edge-01"), satellite("edge-02"), satellite("edge-03")
	a, b := group("A"), group("B")
	member(s1, b)
	member(s1, a)
	member(s2, b)

	used := db.MeasureStatements(t, func() {
		rows, err := q.ListGroupNamesOfSatellites(ctx, []int32{s1, s2, s3})
		require.NoError(t, err)
		require.Equal(t, []database.ListGroupNamesOfSatellitesRow{
			{SatelliteID: s1, GroupName: "A"},
			{SatelliteID: s1, GroupName: "B"},
			{SatelliteID: s2, GroupName: "B"},
		}, rows, "ordered by satellite and group name; a satellite without groups has no row")
	})
	require.EqualValues(t, 1, used.Total, used.String())

	rows, err := q.ListGroupNamesOfSatellites(ctx, []int32{})
	require.NoError(t, err)
	require.Empty(t, rows)
}
