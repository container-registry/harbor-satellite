package server

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/harbortest"
	"github.com/stretchr/testify/require"
)

// budget is the most work one request may cause. The numbers are what the
// code did when the budgets were introduced; changes that remove work lower
// them, nothing raises them without a reason in the commit.
type budget struct {
	Statements int64
	HarborAPI  int
	Registry   int
}

type usage struct {
	statements dbtest.Statements
	calls      []harbortest.Call
}

func (u usage) harborAPI() int {
	n := 0
	for _, c := range u.calls {
		if !c.Registry() {
			n++
		}
	}
	return n
}

func (u usage) registry() int {
	return len(u.calls) - u.harborAPI()
}

func (u usage) String() string {
	routes := harbortest.CountRoutes(u.calls)
	keys := make([]string, 0, len(routes))
	for k := range routes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("statements:\n")
	b.WriteString(u.statements.String())
	b.WriteString("\nharbor calls:\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "%6d  %s\n", routes[k], k)
	}
	return b.String()
}

func (h *gcHarness) measure(op func()) usage {
	h.t.Helper()
	mark := h.harbor.Mark()
	stmts := h.db.MeasureStatements(h.t, op)
	return usage{statements: stmts, calls: h.harbor.CallsSince(mark)}
}

func requireWithinBudget(t *testing.T, op string, got usage, limit budget) {
	t.Helper()
	t.Logf("budget %s: %d statements (max %d), %d Harbor API calls (max %d), %d registry calls (max %d)\n%s",
		op, got.statements.Total, limit.Statements, got.harborAPI(), limit.HarborAPI, got.registry(), limit.Registry, got)
	require.LessOrEqual(t, got.statements.Total, limit.Statements, "%s: SQL statements over budget", op)
	require.LessOrEqual(t, got.harborAPI(), limit.HarborAPI, "%s: Harbor API calls over budget", op)
	require.LessOrEqual(t, got.registry(), limit.Registry, "%s: registry calls over budget", op)
}

func TestBudgetHeartbeat(t *testing.T) {
	h := newGCHarness(t)
	h.createConfig("edge-config")
	h.syncGroup("edge-group", "library/alpine")
	creds := h.onboard("edge-01", "edge-config", "edge-group")

	got := h.measure(func() {
		h.mustDo(http.MethodPost, "/satellites/sync", heartbeatRequest("edge-01"), basicAuth(creds.Username, creds.Password), http.StatusOK)
	})

	requireWithinBudget(t, "heartbeat", got, budget{Statements: 8, HarborAPI: 0, Registry: 0})
}

func TestBudgetZTR(t *testing.T) {
	h := newGCHarness(t)
	h.createConfig("edge-config")
	h.syncGroup("edge-group", "library/alpine")
	token := h.registerSatellite("edge-01", "edge-config", "edge-group")

	got := h.measure(func() { h.ztr(token) })

	requireWithinBudget(t, "ZTR", got, budget{Statements: 9, HarborAPI: 2, Registry: 10})
}

func TestBudgetSyncGroup(t *testing.T) {
	for _, tc := range []struct {
		name    string
		members int
		limit   budget
	}{
		{name: "1 member", members: 1, limit: budget{Statements: 6, HarborAPI: 3, Registry: 19}},
		{name: "100 members", members: 100, limit: budget{Statements: 105, HarborAPI: 201, Registry: 19}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newGCHarness(t)
			h.syncGroup("edge-group", "library/alpine")
			h.seedGroupMembers("edge-group", tc.members)

			got := h.measure(func() { h.syncGroup("edge-group", "library/alpine", "team/app") })

			requireWithinBudget(t, "SyncGroup with "+tc.name, got, tc.limit)
		})
	}
}

func TestBudgetUpdateConfig(t *testing.T) {
	h := newGCHarness(t)
	h.createConfig("edge-config")

	got := h.measure(func() {
		h.mustDo(http.MethodPatch, "/api/configs/edge-config", map[string]any{"app_config": map[string]any{"log_level": "debug"}}, h.admin, http.StatusOK)
	})

	requireWithinBudget(t, "UpdateConfig", got, budget{Statements: 5, HarborAPI: 1, Registry: 19})
}

// seedGroupMembers adds n satellites with robot accounts to a group directly
// in the database and the Harbor stub, as registration would have left them.
func (h *gcHarness) seedGroupMembers(group string, n int) {
	h.t.Helper()
	ctx := context.Background()
	q := database.New(h.db.DB)
	grp, err := q.GetGroupByName(ctx, group)
	require.NoError(h.t, err)
	for i := range n {
		name := fmt.Sprintf("member-%03d", i)
		sat, err := q.CreateSatellite(ctx, name)
		require.NoError(h.t, err)
		robot := h.harbor.SeedRobot(name, "satellite")
		_, err = q.AddRobotAccount(ctx, database.AddRobotAccountParams{
			RobotName:       robot.Name,
			RobotSecretHash: "not-a-real-hash",
			RobotID:         strconv.FormatInt(robot.ID, 10),
			SatelliteID:     sat.ID,
		})
		require.NoError(h.t, err)
		require.NoError(h.t, q.AddSatelliteToGroup(ctx, database.AddSatelliteToGroupParams{SatelliteID: sat.ID, GroupID: grp.ID}))
	}
}
