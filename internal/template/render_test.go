package template

import "testing"

func TestRenderStringSubstitutesData(t *testing.T) {
	m := &Manager{}
	data := map[string]any{"Conversation": map[string]any{"ReferenceNumber": 42, "Subject": "<b>Help</b>"}}
	got := m.RenderString(data, `Ticket #{{ .Conversation.ReferenceNumber }}: {{ .Conversation.Subject }}`)
	if want := "Ticket #42: <b>Help</b>"; got != want {
		t.Fatalf("RenderString = %q, want %q", got, want)
	}
}
