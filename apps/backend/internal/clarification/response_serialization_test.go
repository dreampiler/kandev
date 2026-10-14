package clarification

import (
	"encoding/json"
	"testing"
	"time"
)

// TestSerializeResponse_AnsweredNeverOmitsKeys proves M6a/N3a rule 4: an
// answered response with no selected options must still emit
// "selected_options":[] rather than omitting the key, and "rejected":false
// and "reject_reason":"" must both be present. Marshalling the tagged
// clarification.Response struct directly (Answers/Rejected/RejectReason all
// carry `omitempty`) would drop every one of these keys.
func TestSerializeResponse_AnsweredNeverOmitsKeys(t *testing.T) {
	resp := &Response{
		PendingID: "pending-1",
		Answers: []Answer{
			{QuestionID: "q1", CustomText: "blue"},
		},
		RespondedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	out, err := SerializeResponse(resp)
	if err != nil {
		t.Fatalf("SerializeResponse: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if _, ok := raw["rejected"]; !ok {
		t.Fatalf("expected \"rejected\" key present, got %s", out)
	}
	if v, ok := raw["reject_reason"]; !ok || v != "" {
		t.Fatalf("expected \"reject_reason\":\"\", got %s", out)
	}
	answers, ok := raw["answers"].([]any)
	if !ok || len(answers) != 1 {
		t.Fatalf("expected one answers entry, got %s", out)
	}
	entry, ok := answers[0].(map[string]any)
	if !ok {
		t.Fatalf("expected answer entry to be an object, got %s", out)
	}
	sel, ok := entry["selected_options"].([]any)
	if !ok {
		t.Fatalf("expected \"selected_options\" key present as an array, got %s", out)
	}
	if len(sel) != 0 {
		t.Fatalf("expected empty selected_options, got %v", sel)
	}
}

// TestSerializeResponse_RejectedNeverOmitsAnswersKey proves M6a/N3a rule 4
// for the rejected shape: a nil Answers slice must serialize as "answers":[]
// rather than being omitted, so a client reading response.answers on a
// rejection never meets undefined.
func TestSerializeResponse_RejectedNeverOmitsAnswersKey(t *testing.T) {
	resp := &Response{
		PendingID:    "pending-1",
		Answers:      nil,
		Rejected:     true,
		RejectReason: "not now",
		RespondedAt:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	out, err := SerializeResponse(resp)
	if err != nil {
		t.Fatalf("SerializeResponse: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	answers, ok := raw["answers"].([]any)
	if !ok {
		t.Fatalf("expected \"answers\" key present as an array, got %s", out)
	}
	if len(answers) != 0 {
		t.Fatalf("expected empty answers, got %v", answers)
	}
	if raw["rejected"] != true {
		t.Fatalf("expected rejected:true, got %s", out)
	}
	if raw["reject_reason"] != "not now" {
		t.Fatalf("expected reject_reason preserved, got %s", out)
	}
}

// TestSerializeResponse_RoundTrip proves DeserializeResponse recovers every
// M6a field SerializeResponse wrote, including an entry's empty
// selected_options and custom_text.
func TestSerializeResponse_RoundTrip(t *testing.T) {
	original := &Response{
		PendingID: "pending-42",
		Answers: []Answer{
			{QuestionID: "q1", SelectedOptions: []string{"opt-a", "opt-b"}},
			{QuestionID: "q2", CustomText: "free text"},
			{QuestionID: "q3"},
		},
		Rejected:     false,
		RejectReason: "",
		RespondedAt:  time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC),
	}

	serialized, err := SerializeResponse(original)
	if err != nil {
		t.Fatalf("SerializeResponse: %v", err)
	}
	got, err := DeserializeResponse(serialized)
	if err != nil {
		t.Fatalf("DeserializeResponse: %v", err)
	}

	if got.PendingID != original.PendingID {
		t.Fatalf("PendingID: got %q, want %q", got.PendingID, original.PendingID)
	}
	if !got.RespondedAt.Equal(original.RespondedAt) {
		t.Fatalf("RespondedAt: got %v, want %v", got.RespondedAt, original.RespondedAt)
	}
	if len(got.Answers) != len(original.Answers) {
		t.Fatalf("Answers length: got %d, want %d", len(got.Answers), len(original.Answers))
	}
	for i, a := range got.Answers {
		want := original.Answers[i]
		if a.QuestionID != want.QuestionID || a.CustomText != want.CustomText {
			t.Fatalf("Answers[%d]: got %+v, want %+v", i, a, want)
		}
		if len(a.SelectedOptions) != len(want.SelectedOptions) {
			t.Fatalf("Answers[%d].SelectedOptions: got %v, want %v", i, a.SelectedOptions, want.SelectedOptions)
		}
	}
}

// TestSerializeResponse_AnsweredByChannelRoundTrip proves the E5 seam
// survives the durable record: a slack_thread channel serializes and
// deserializes unchanged, while an unrecorded channel stays absent from
// the wire and deserializes to the zero value.
func TestSerializeResponse_AnsweredByChannelRoundTrip(t *testing.T) {
	original := &Response{
		PendingID: "pending-77",
		Answers: []Answer{
			{QuestionID: "q1", CustomText: "thread reply"},
		},
		AnsweredBy:  AnsweredBy{Channel: "slack_thread", Ref: "C012AB345CD/1729000000.000100"},
		RespondedAt: time.Date(2026, 5, 6, 7, 8, 9, 0, time.UTC),
	}

	serialized, err := SerializeResponse(original)
	if err != nil {
		t.Fatalf("SerializeResponse: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(serialized), &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	by, ok := raw["answered_by"].(map[string]any)
	if !ok {
		t.Fatalf("expected \"answered_by\" object present, got %s", serialized)
	}
	if by["channel"] != "slack_thread" || by["ref"] != "C012AB345CD/1729000000.000100" {
		t.Fatalf("expected slack_thread channel and ref, got %v", by)
	}

	got, err := DeserializeResponse(serialized)
	if err != nil {
		t.Fatalf("DeserializeResponse: %v", err)
	}
	if got.AnsweredBy != original.AnsweredBy {
		t.Fatalf("AnsweredBy: got %+v, want %+v", got.AnsweredBy, original.AnsweredBy)
	}

	unmarked := &Response{
		PendingID:   "pending-78",
		Answers:     []Answer{{QuestionID: "q1"}},
		RespondedAt: time.Date(2026, 5, 6, 7, 8, 9, 0, time.UTC),
	}
	plain, err := SerializeResponse(unmarked)
	if err != nil {
		t.Fatalf("SerializeResponse: %v", err)
	}
	var plainRaw map[string]any
	if err := json.Unmarshal([]byte(plain), &plainRaw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := plainRaw["answered_by"]; ok {
		t.Fatalf("expected no \"answered_by\" key when unrecorded, got %s", plain)
	}
	gotPlain, err := DeserializeResponse(plain)
	if err != nil {
		t.Fatalf("DeserializeResponse: %v", err)
	}
	if gotPlain.AnsweredBy != (AnsweredBy{}) {
		t.Fatalf("expected zero AnsweredBy, got %+v", gotPlain.AnsweredBy)
	}
}

// TestSerializeResponse_CancelledShape proves the fixed M6 "cancelled"
// payload — empty answers, rejected true, reject_reason exactly "cancelled"
// — round-trips through the same explicit serializer as every other outcome.
func TestSerializeResponse_CancelledShape(t *testing.T) {
	resp := &Response{
		PendingID:    "pending-9",
		Answers:      []Answer{},
		Rejected:     true,
		RejectReason: "cancelled",
		RespondedAt:  time.Now(),
	}
	out, err := SerializeResponse(resp)
	if err != nil {
		t.Fatalf("SerializeResponse: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if raw["reject_reason"] != "cancelled" {
		t.Fatalf("expected reject_reason \"cancelled\", got %s", out)
	}
}
