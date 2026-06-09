package worker

import "testing"

// TestSanitizeName locks the controller-mirror contract: SanitizeName must
// produce identical output to render_jetstream.go's sanitizeName for all
// inputs that a TMIComponent CR name can take.
func TestSanitizeName(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"tmi-extractor", "TMI_EXTRACTOR"},
		{"tmi-chunk-embed", "TMI_CHUNK_EMBED"},
		{"tmi.extractor", "TMI_EXTRACTOR"},
		{"tmi chunk embed", "TMI_CHUNK_EMBED"},
		{"already_upper", "ALREADY_UPPER"},
	}
	for _, tc := range cases {
		got := SanitizeName(tc.input)
		if got != tc.want {
			t.Errorf("SanitizeName(%q) = %q; want %q", tc.input, got, tc.want)
		}
	}
}

func TestStreamNameFor(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"tmi-extractor", "TMI_TMI_EXTRACTOR"},
		{"tmi-chunk-embed", "TMI_TMI_CHUNK_EMBED"},
	}
	for _, tc := range cases {
		got := StreamNameFor(tc.input)
		if got != tc.want {
			t.Errorf("StreamNameFor(%q) = %q; want %q", tc.input, got, tc.want)
		}
	}
}

func TestConsumerNameFor(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"tmi-extractor", "TMI_EXTRACTOR_CONSUMER"},
		{"tmi-chunk-embed", "TMI_CHUNK_EMBED_CONSUMER"},
	}
	for _, tc := range cases {
		got := ConsumerNameFor(tc.input)
		if got != tc.want {
			t.Errorf("ConsumerNameFor(%q) = %q; want %q", tc.input, got, tc.want)
		}
	}
}

func TestResultSubject(t *testing.T) {
	got := ResultSubject("j1")
	want := "jobs.result.j1"
	if got != want {
		t.Errorf("ResultSubject(%q) = %q; want %q", "j1", got, want)
	}
}

func TestChunkEmbedSubject(t *testing.T) {
	got := ChunkEmbedSubject("j2")
	want := "jobs.chunkembed.j2"
	if got != want {
		t.Errorf("ChunkEmbedSubject(%q) = %q; want %q", "j2", got, want)
	}
}

func TestHeartbeatSubject(t *testing.T) {
	got := HeartbeatSubject("tmi-extractor")
	want := "components.heartbeat.tmi-extractor"
	if got != want {
		t.Errorf("HeartbeatSubject(%q) = %q; want %q", "tmi-extractor", got, want)
	}
}

func TestDLQConstants(t *testing.T) {
	if DLQStream != "TMI_DLQ" {
		t.Errorf("DLQStream = %q; want %q", DLQStream, "TMI_DLQ")
	}
	if DLQAdvisoryStream != "TMI_DLQ_ADVISORY" {
		t.Errorf("DLQAdvisoryStream = %q; want %q", DLQAdvisoryStream, "TMI_DLQ_ADVISORY")
	}
	if SubjectMaxDeliverAdvisory != "$JS.EVENT.ADVISORY.CONSUMER.MAX_DELIVERIES.>" {
		t.Errorf("SubjectMaxDeliverAdvisory = %q; want %q",
			SubjectMaxDeliverAdvisory, "$JS.EVENT.ADVISORY.CONSUMER.MAX_DELIVERIES.>")
	}
	if SubjectDLQ != "jobs.dlq" {
		t.Errorf("SubjectDLQ = %q; want %q", SubjectDLQ, "jobs.dlq")
	}
}
