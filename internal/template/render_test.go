package template

import "testing"

func TestRenderStringPreservesOriginalOnParseError(t *testing.T) {
	m := &Manager{}
	content := `Hello {{ .Name }}; {{ if }}broken{{ end }}`
	if got := m.RenderString(map[string]any{"Name": "Ada"}, content); got != content {
		t.Fatalf("RenderString = %q, want original %q", got, content)
	}
}

func TestRenderStringSubstitutesData(t *testing.T) {
	m := &Manager{}
	data := map[string]any{"Conversation": map[string]any{"ReferenceNumber": 42, "Subject": "<b>Help</b>"}}
	got := m.RenderString(data, `Ticket #{{ .Conversation.ReferenceNumber }}: {{ .Conversation.Subject }}`)
	if want := "Ticket #42: <b>Help</b>"; got != want {
		t.Fatalf("RenderString = %q, want %q", got, want)
	}

}

func TestRenderStringMissingMapKeys(t *testing.T) {
	m := &Manager{}
	data := map[string]any{"Conversation": map[string]any{}}
	got := m.RenderString(data, `{{ .Missing }}|{{ .Missing.Name }}|{{ .Conversation.Subject }}`)
	if want := "<no value>|<no value>|<no value>"; got != want {
		t.Fatalf("RenderString = %q, want %q", got, want)
	}
}
