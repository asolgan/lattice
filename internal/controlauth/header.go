// Package controlauth is the shared home for control-plane request
// authentication (control-plane-capability-authz-design.md). Fire 1a adds
// the actor-on-the-wire header carried by the three control services
// (Weaver/Loom/Refractor) and their CLI/Loupe clients; Fire 1b adds the
// capability checker that authorizes the extracted actor.
package controlauth

import (
	nats "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/micro"
)

// HeaderActor is the NATS message header a control-plane client stamps with
// the calling operator's actor key (the full `vtx.identity.<id>` key,
// matching the write-path OperationEnvelope.Actor value). Absent/empty means
// no actor was asserted.
const HeaderActor = "Lattice-Actor"

// ActorFromRequest extracts HeaderActor from a micro.Request. Returns "" when
// the header is absent, empty, or the request carries no headers at all.
func ActorFromRequest(req micro.Request) string {
	if h := req.Headers(); h != nil {
		return h.Get(HeaderActor)
	}
	return ""
}

// NewActorRequestMsg builds a NATS message addressed to subject with an
// empty body and, when actor is non-empty, a HeaderActor header carrying it.
// Shared by every control-plane client (CLI + Loupe) so the header name and
// shape never drift between callers.
func NewActorRequestMsg(subject, actor string) *nats.Msg {
	msg := &nats.Msg{Subject: subject}
	if actor != "" {
		msg.Header = nats.Header{HeaderActor: {actor}}
	}
	return msg
}
