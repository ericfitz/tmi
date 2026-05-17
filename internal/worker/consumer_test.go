package worker

import (
	"errors"
	"testing"

	"github.com/ericfitz/tmi/pkg/jobenvelope"
)

func TestHandlerOutcome(t *testing.T) {
	// A handler returning nil yields OutcomeAck.
	if got := outcomeFor(nil); got != OutcomeAck {
		t.Fatalf("nil error: got %v", got)
	}
	// A terminal JobError yields OutcomeTerm: the job will never succeed on
	// redelivery, so terminate it.
	je := &JobError{ReasonCode: "extraction_malformed", Terminal: true}
	if got := outcomeFor(je); got != OutcomeTerm {
		t.Fatalf("terminal JobError: got %v", got)
	}
	// A non-terminal error (transient) yields OutcomeNak for redelivery.
	if got := outcomeFor(errors.New("transient")); got != OutcomeNak {
		t.Fatalf("transient error: got %v", got)
	}
	// A non-terminal JobError also yields OutcomeNak.
	je2 := &JobError{ReasonCode: "x", Terminal: false}
	if got := outcomeFor(je2); got != OutcomeNak {
		t.Fatalf("non-terminal JobError: got %v", got)
	}
}

func TestJobErrorString(t *testing.T) {
	je := &JobError{ReasonCode: "extraction_malformed", Detail: "bad zip", Terminal: true}
	if je.Error() == "" {
		t.Fatal("JobError.Error() empty")
	}
}

func TestIdempotency(t *testing.T) {
	idem := newIdempotency()
	if idem.done("j1") {
		t.Fatal("fresh idempotency reports j1 done")
	}
	idem.mark("j1")
	if !idem.done("j1") {
		t.Fatal("after mark, j1 should be done")
	}
	if idem.done("j2") {
		t.Fatal("j2 should not be done")
	}
}

// keep jobenvelope imported — JobHandler references it.
var _ = jobenvelope.Job{}
