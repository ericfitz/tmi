package api

import (
	"encoding/json"

	"github.com/ericfitz/tmi/internal/worker"
	"github.com/ericfitz/tmi/pkg/jobenvelope"
)

// maxDeliverAdvisory is the subset of a JetStream MAX_DELIVERIES consumer
// advisory the DLQ producer needs. Parsed from a local struct rather than a
// nats-server type to avoid a server dependency. The advisory carries the
// source stream + sequence but NOT the original payload, which is why the
// producer recovers it via GetMsg.
type maxDeliverAdvisory struct {
	Stream     string `json:"stream"`
	Consumer   string `json:"consumer"`
	StreamSeq  uint64 `json:"stream_seq"`
	Deliveries uint64 `json:"deliveries"`
}

// parseMaxDeliverAdvisory decodes a MAX_DELIVERIES advisory payload.
func parseMaxDeliverAdvisory(data []byte) (maxDeliverAdvisory, error) {
	var adv maxDeliverAdvisory
	if err := json.Unmarshal(data, &adv); err != nil {
		return maxDeliverAdvisory{}, err
	}
	return adv, nil
}

// isSelfReferentialStream reports whether advisories for the given source
// stream must be ignored to avoid dead-letter loops: the result stream and
// the DLQ stream are consumed by the monolith itself, never dead-lettered.
func isSelfReferentialStream(stream string) bool {
	return stream == worker.ResultStream || stream == worker.DLQStream
}

// decodeJobForDLQ decodes recovered source bytes as a Job envelope and returns
// it only when it is a valid job. This scopes dead-lettering to job streams
// without hardcoding component names: a Result envelope or any non-job message
// fails jobenvelope.Validate and is skipped.
func decodeJobForDLQ(data []byte) (jobenvelope.Job, bool) {
	var job jobenvelope.Job
	if err := json.Unmarshal(data, &job); err != nil {
		return jobenvelope.Job{}, false
	}
	if err := jobenvelope.Validate(job); err != nil {
		return jobenvelope.Job{}, false
	}
	return job, true
}
