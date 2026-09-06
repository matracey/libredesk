package template

import (
	"html/template"
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
