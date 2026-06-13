package weaver

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/asolgan/lattice/internal/testutil"
	internalweaver "github.com/asolgan/lattice/internal/weaver"
	"github.com/asolgan/lattice/internal/weaver/control"
)

// fakeEngine satisfies the control package's unexported engineControl
// interface structurally, mirroring internal/weaver/control's own test
// fake. Lets this package's tests drive a real weaver-control NATS
// responder without a *weaver.Engine.
type fakeEngine struct {
	targets []internalweaver.TargetSummary
	errOn   map[string]error
}

func (f *fakeEngine) ListTargets(_ context.Context) ([]internalweaver.TargetSummary, error) {
	return f.targets, nil
}

func (f *fakeEngine) Disable(_ context.Context, targetID string) error {
	return f.errOn["disable:"+targetID]
}

func (f *fakeEngine) Enable(_ context.Context, targetID string) error {
	return f.errOn["enable:"+targetID]
}

func (f *fakeEngine) Revoke(_ context.Context, targetID string) error {
	return f.errOn["revoke:"+targetID]
}

// startWeaverControlTest starts an embedded NATS server with a
// weaver-control responder backed by eng, and returns its NATS URL.
func startWeaverControlTest(t *testing.T, eng *fakeEngine) string {
	t.Helper()
	url := testutil.StartEmbeddedNATS(t)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	conn, err := connectRaw(t, url)
	require.NoError(t, err)

	svc := control.NewService(eng, nil, testutil.TestLogger())
	require.NoError(t, svc.StartNATSListener(ctx, conn))

	return url
}

// connectRaw opens a plain *nats.Conn for the control service under test.
func connectRaw(t *testing.T, url string) (*nats.Conn, error) {
	t.Helper()
	return nats.Connect(url)
}

// runCmd executes cmd with args, capturing stdout. Returns stdout and the
// command error.
func runCmd(t *testing.T, cmd *cobra.Command, args []string) (string, error) {
	t.Helper()
	cmd.SetArgs(args)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	cmdErr := cmd.Execute()

	require.NoError(t, w.Close())
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String(), cmdErr
}

func TestWeaverList_HappyPath(t *testing.T) {
	eng := &fakeEngine{
		targets: []internalweaver.TargetSummary{
			{TargetID: "t1", LensRef: "lens-1", Gaps: []string{"missing_a"}, State: "active"},
			{TargetID: "t2", LensRef: "lens-2", Gaps: []string{"missing_b"}, State: "disabled"},
		},
		errOn: map[string]error{},
	}
	url := startWeaverControlTest(t, eng)

	natsURL := url
	outputFmt := "json"
	cmd := NewCommand(&natsURL, &outputFmt)

	out, err := runCmd(t, cmd, []string{"list"})
	require.NoError(t, err)
	assert.Contains(t, out, "t1")
	assert.Contains(t, out, "t2")
	assert.Contains(t, out, "active")
	assert.Contains(t, out, "disabled")
}

func TestWeaverList_Empty(t *testing.T) {
	eng := &fakeEngine{errOn: map[string]error{}}
	url := startWeaverControlTest(t, eng)

	natsURL := url
	outputFmt := ""
	cmd := NewCommand(&natsURL, &outputFmt)

	out, err := runCmd(t, cmd, []string{"list"})
	require.NoError(t, err)
	assert.Contains(t, out, "no registered targets")
}

func TestWeaverDisable_HappyPath(t *testing.T) {
	eng := &fakeEngine{errOn: map[string]error{}}
	url := startWeaverControlTest(t, eng)

	natsURL := url
	outputFmt := ""
	cmd := NewCommand(&natsURL, &outputFmt)

	out, err := runCmd(t, cmd, []string{"disable", "t1"})
	require.NoError(t, err)
	assert.Contains(t, out, `target "t1" disabled`)
}

func TestWeaverEnable_HappyPath(t *testing.T) {
	eng := &fakeEngine{errOn: map[string]error{}}
	url := startWeaverControlTest(t, eng)

	natsURL := url
	outputFmt := ""
	cmd := NewCommand(&natsURL, &outputFmt)

	out, err := runCmd(t, cmd, []string{"enable", "t1"})
	require.NoError(t, err)
	assert.Contains(t, out, `target "t1" enabled`)
}

func TestWeaverRevoke_HappyPath(t *testing.T) {
	eng := &fakeEngine{errOn: map[string]error{}}
	url := startWeaverControlTest(t, eng)

	natsURL := url
	outputFmt := ""
	cmd := NewCommand(&natsURL, &outputFmt)

	out, err := runCmd(t, cmd, []string{"revoke", "t1"})
	require.NoError(t, err)
	assert.Contains(t, out, `target "t1" revoked`)
}

func TestWeaverDisable_NotRegistered_JSON(t *testing.T) {
	eng := &fakeEngine{errOn: map[string]error{
		"disable:ghost": errors.New(`weaver: target "ghost" not registered`),
	}}
	url := startWeaverControlTest(t, eng)

	natsURL := url
	outputFmt := "json"
	cmd := NewCommand(&natsURL, &outputFmt)

	out, err := runCmd(t, cmd, []string{"disable", "ghost"})
	require.Error(t, err)
	assert.Contains(t, out, "ghost")
	assert.Contains(t, out, `"ok":false`)
}
