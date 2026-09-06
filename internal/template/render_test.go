package template

import (
	"html/template"
	"strings"
	"testing"

	"github.com/abhinavxd/libredesk/internal/testutil"
	"github.com/jmoiron/sqlx"
	"github.com/zerodha/logf"
)

func newRenderManager(t *testing.T) (*Manager, *sqlx.DB) {
	t.Helper()
	db := testutil.NewDB(t, "template_render")
	lo := logf.New(logf.Opts{})
	funcs := template.FuncMap{
		"RootURL":  func() string { return "https://desk.example" },
		"SiteName": func() string { return "Support" },
	}
	m, err := New(&lo, db, nil, nil, funcs, testutil.NewI18n(t))
	if err != nil {
		t.Fatalf("creating template manager: %v", err)
	}
	return m, db
}

func TestRenderStoredEmailTemplateRejectsMalformedSubject(t *testing.T) {
	m, db := newRenderManager(t)
	db.MustExec(`UPDATE templates SET subject = $1, body = $2 WHERE name = $3`,
		`Hello {{ .Name }}; {{ if }}broken{{ end }}`, `Body for {{ .Name }}`, TmplConversationAssigned)
	body, subject, err := m.RenderStoredEmailTemplate(TmplConversationAssigned, map[string]any{"Name": "Ada"})
	if err == nil || !strings.Contains(err.Error(), "parsing subject template:") || body != "" || subject != "" {
		t.Fatalf("RenderStoredEmailTemplate = %q, %q, %v; want empty outputs and subject parse error", body, subject, err)
	}
}

func TestRenderStoredEmailTemplateMissingName(t *testing.T) {
	m, _ := newRenderManager(t)
	body, subject, err := m.RenderStoredEmailTemplate("absent-template", nil)
	if err == nil || err.Error() != "template absent-template not found" || body != "" || subject != "" {
		t.Fatalf("RenderStoredEmailTemplate = %q, %q, %v; want empty outputs and missing-name error", body, subject, err)
	}
}

func TestRenderStoredEmailTemplateRendersSeededSubjectAndBody(t *testing.T) {
	m, _ := newRenderManager(t)
	data := map[string]any{
		"Conversation": map[string]any{"ReferenceNumber": 42, "UUID": "ticket-42"},
		"SLA":          map[string]any{"Metric": "first response", "OverdueBy": "5 minutes"},
	}
	body, subject, err := m.RenderStoredEmailTemplate(TmplSLABreached, data)
	if want := "Urgent: SLA Breach for Conversation 42 for first response"; err != nil || subject != want {
		t.Fatalf("RenderStoredEmailTemplate subject = %q, %v; want %q", subject, err, want)
	}
	for _, want := range []string{"conversation 42 has been breached", "for first response", "Overdue by: 5 minutes", "https://desk.example/inboxes/assigned/conversation/ticket-42"} {
		if !strings.Contains(body, want) {
			t.Errorf("body = %q, want substring %q", body, want)
		}
	}
	if strings.Contains(body, "{{") {
		t.Errorf("body contains unrendered template actions: %q", body)
	}
}

func TestRenderEmailWithTemplateRejectsInvalidFieldAccess(t *testing.T) {
	m, _ := newRenderManager(t)
	got, err := m.RenderEmailWithTemplate(map[string]any{"Name": "Ada"}, `Hello {{ .Name }}; {{ .Name.Missing }} tail`)
	if err == nil || !strings.Contains(err.Error(), "executing base template:") || got != "" {
		t.Fatalf("RenderEmailWithTemplate = %q, %v; want empty output and execution error", got, err)
	}
}

func TestRenderEmailWithTemplateUsesFunctionsInBaseAndContent(t *testing.T) {
	m, db := newRenderManager(t)
	db.MustExec(`INSERT INTO templates (name, type, is_default, body) VALUES ('wrapper', 'email_outgoing', true, $1)`,
		`{{ SiteName }}: {{ template "content" . }}`)
	got, err := m.RenderEmailWithTemplate(nil, `Visit {{ RootURL }}`)
	if want := "Support: Visit https://desk.example"; err != nil || got != want {
		t.Fatalf("RenderEmailWithTemplate = %q, %v; want %q", got, err, want)
	}
}

func TestRenderEmailWithTemplateRendersMissingMapKeys(t *testing.T) {
	m, _ := newRenderManager(t)
	data := map[string]any{"Conversation": map[string]any{}}
	got, err := m.RenderEmailWithTemplate(data, `{{ .Missing.Name }}|{{ .Conversation.Subject }}`)
	if want := "<no value>|<no value>"; err != nil || got != want {
		t.Fatalf("RenderEmailWithTemplate = %q, %v; want %q without error", got, err, want)
	}
}

func TestRenderEmailWithTemplateRejectsMalformedContent(t *testing.T) {
	m, _ := newRenderManager(t)
	got, err := m.RenderEmailWithTemplate(nil, `prefix {{ if }}broken{{ end }}`)
	if err == nil || !strings.Contains(err.Error(), "parsing content template:") || got != "" {
		t.Fatalf("RenderEmailWithTemplate = %q, %v; want empty output and content parse error", got, err)
	}
}

func TestRenderEmailWithTemplateWrapsContent(t *testing.T) {
	m, db := newRenderManager(t)
	db.MustExec(`INSERT INTO templates (name, type, is_default, body) VALUES ('wrapper', 'email_outgoing', true, $1)`,
		`<header>{{ .Name }}</header><main>{{ template "content" . }}</main>`)
	got, err := m.RenderEmailWithTemplate(map[string]any{"Name": "Ada"}, `<p>Hello {{ .Name }}</p>`)
	if want := "<header>Ada</header><main><p>Hello Ada</p></main>"; err != nil || got != want {
		t.Fatalf("RenderEmailWithTemplate = %q, %v; want %q", got, err, want)
	}
}

func TestRenderEmailWithTemplateWithoutDefault(t *testing.T) {
	m, db := newRenderManager(t)
	db.MustExec(`DELETE FROM templates WHERE is_default`)
	got, err := m.RenderEmailWithTemplate(map[string]any{"Name": "Ada"}, `<p>Hello {{ .Name }}</p>`)
	if err != nil || got != "<p>Hello Ada</p>" {
		t.Fatalf("RenderEmailWithTemplate = %q, %v; want unwrapped content", got, err)
	}
}

func TestRenderStringUsesFunctionMap(t *testing.T) {
	m := &Manager{funcMap: template.FuncMap{"SiteName": func() string { return "Support" }}}
	if got := m.RenderString(nil, `Welcome to {{ SiteName }}`); got != "Welcome to Support" {
		t.Fatalf("RenderString = %q, want %q", got, "Welcome to Support")
	}
}

func TestRenderStringPreservesEntireOriginalOnExecutionError(t *testing.T) {
	m := &Manager{}
	content := `Hello {{ .Name }}; {{ .Name.Missing }} tail`
	if got := m.RenderString(map[string]any{"Name": "Ada"}, content); got != content {
		t.Fatalf("RenderString = %q, want entire original %q", got, content)
	}
}

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
