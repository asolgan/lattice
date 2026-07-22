package control_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	nats "github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/operatinggraph/lattice/internal/controlauth"
	"github.com/operatinggraph/lattice/internal/refractor/control"
)

type mockReprojector struct {
	res    control.Reprojection
	err    error
	actors []string
}

func (m *mockReprojector) Reproject(_ context.Context, actorKey string) (control.Reprojection, error) {
	m.actors = append(m.actors, actorKey)
	if m.err != nil {
		return control.Reprojection{}, m.err
	}
	res := m.res
	res.Actor = actorKey
	return res, nil
}

// reprojectRequest sends a reproject control RPC for ruleID and decodes the reply.
func reprojectRequest(t *testing.T, nc *nats.Conn, ruleID, actorKey string) control.ControlResponse {
	t.Helper()
	subj := control.ControlSubject(ruleID, "reproject")
	msg := controlauth.NewActorRequestMsg(subj, "vtx.identity.OPERATOR")
	body, err := json.Marshal(control.ControlRequest{ActorKey: actorKey})
	require.NoError(t, err)
	msg.Data = body

	reply, err := nc.RequestMsg(msg, 3*time.Second)
	require.NoError(t, err)
	var resp control.ControlResponse
	require.NoError(t, json.Unmarshal(reply.Data, &resp))
	return resp
}

func TestControl_Reproject_HealsRegisteredActorAggregate(t *testing.T) {
	nc, _ := startControlTestServerConn(t)
	svc := control.NewService()
	svc.SetCapabilityChecker(control.NewStubCapabilityChecker(nil))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, svc.StartNATSListener(ctx, nc))

	mr := &mockReprojector{res: control.Reprojection{Wrote: true, ProjectionSeq: 91}}
	svc.RegisterReprojector("rule-reproj", mr)

	resp := reprojectRequest(t, nc, "rule-reproj", "vtx.identity.Areproject1Aaaaaaaaa")
	require.Empty(t, resp.Error)
	require.NotNil(t, resp.Reproject)
	assert.True(t, resp.Reproject.Wrote)
	assert.False(t, resp.Reproject.Converged)
	assert.Equal(t, uint64(91), resp.Reproject.ProjectionSeq)
	assert.Equal(t, []string{"vtx.identity.Areproject1Aaaaaaaaa"}, mr.actors)
}

func TestControl_Reproject_ReportsConvergedWithoutError(t *testing.T) {
	// A converged actor is the steady-state result, not a failure — the sweep
	// must be able to tell "nothing to do" from "could not do it".
	nc, _ := startControlTestServerConn(t)
	svc := control.NewService()
	svc.SetCapabilityChecker(control.NewStubCapabilityChecker(nil))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, svc.StartNATSListener(ctx, nc))

	svc.RegisterReprojector("rule-conv", &mockReprojector{res: control.Reprojection{Converged: true}})

	resp := reprojectRequest(t, nc, "rule-conv", "vtx.identity.Areproject1Aaaaaaaaa")
	require.Empty(t, resp.Error)
	require.NotNil(t, resp.Reproject)
	assert.True(t, resp.Reproject.Converged)
	assert.False(t, resp.Reproject.Wrote)
}

func TestControl_Reproject_RefusesUnregisteredLens(t *testing.T) {
	// Only actor-aggregate lenses register a Reprojector, so an unregistered
	// rule is both the "unknown lens" and the "wrong lens kind" answer. It
	// must fail closed rather than silently no-op.
	nc, _ := startControlTestServerConn(t)
	svc := control.NewService()
	svc.SetCapabilityChecker(control.NewStubCapabilityChecker(nil))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, svc.StartNATSListener(ctx, nc))

	resp := reprojectRequest(t, nc, "rule-plain", "vtx.identity.Areproject1Aaaaaaaaa")
	require.NotEmpty(t, resp.Error)
	assert.Contains(t, resp.Error, "actor-aggregate")
	assert.Nil(t, resp.Reproject)
}

func TestControl_Reproject_RequiresActorKey(t *testing.T) {
	nc, _ := startControlTestServerConn(t)
	svc := control.NewService()
	svc.SetCapabilityChecker(control.NewStubCapabilityChecker(nil))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, svc.StartNATSListener(ctx, nc))

	mr := &mockReprojector{}
	svc.RegisterReprojector("rule-noactor", mr)

	resp := reprojectRequest(t, nc, "rule-noactor", "")
	require.NotEmpty(t, resp.Error)
	assert.Contains(t, resp.Error, "actorKey")
	assert.Empty(t, mr.actors, "the reprojector must not be invoked without an actor")
}

func TestControl_Reproject_SurfacesReprojectorError(t *testing.T) {
	nc, _ := startControlTestServerConn(t)
	svc := control.NewService()
	svc.SetCapabilityChecker(control.NewStubCapabilityChecker(nil))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, svc.StartNATSListener(ctx, nc))

	svc.RegisterReprojector("rule-err", &mockReprojector{err: errors.New("target unreachable")})

	resp := reprojectRequest(t, nc, "rule-err", "vtx.identity.Areproject1Aaaaaaaaa")
	require.NotEmpty(t, resp.Error)
	assert.Contains(t, resp.Error, "target unreachable")
}

func TestControl_Reproject_UnregisterRemovesHandle(t *testing.T) {
	nc, _ := startControlTestServerConn(t)
	svc := control.NewService()
	svc.SetCapabilityChecker(control.NewStubCapabilityChecker(nil))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, svc.StartNATSListener(ctx, nc))

	svc.RegisterReprojector("rule-cycle", &mockReprojector{res: control.Reprojection{Wrote: true}})
	require.Empty(t, reprojectRequest(t, nc, "rule-cycle", "vtx.identity.Areproject1Aaaaaaaaa").Error)

	svc.UnregisterReprojector("rule-cycle")
	resp := reprojectRequest(t, nc, "rule-cycle", "vtx.identity.Areproject1Aaaaaaaaa")
	require.NotEmpty(t, resp.Error, "an unregistered lens must fail closed after hot-reload")
}

func TestControl_RegisterReprojector_NilPanics(t *testing.T) {
	svc := control.NewService()
	assert.Panics(t, func() { svc.RegisterReprojector("rule-nil", nil) })
}
