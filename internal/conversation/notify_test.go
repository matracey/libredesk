package conversation

import (
	"html/template"
	"strings"
	"testing"

	"github.com/abhinavxd/libredesk/internal/conversation/models"
	tmpl "github.com/abhinavxd/libredesk/internal/template"
	"github.com/abhinavxd/libredesk/internal/testutil"
	umodels "github.com/abhinavxd/libredesk/internal/user/models"
	"github.com/volatiletech/null/v9"
	"github.com/zerodha/logf"
)

type notifyUserStore struct {
	t         *testing.T
	recipient umodels.User
}

var _ userStore = (*notifyUserStore)(nil)

func (s *notifyUserStore) GetAgentCachedOrLoad(id int) (umodels.User, error) {
	s.t.Helper()
	if id != s.recipient.ID {
		s.t.Fatalf("unexpected recipient ID %d", id)
	}
	return s.recipient, nil
}

func (s *notifyUserStore) Get(int, string, []string) (umodels.User, error) {
	s.t.Helper()
	s.t.Fatal("unexpected userStore.Get call")
	return umodels.User{}, nil
}

func (s *notifyUserStore) GetAgent(int, string) (umodels.User, error) {
	s.t.Helper()
	s.t.Fatal("unexpected userStore.GetAgent call")
	return umodels.User{}, nil
}

func (s *notifyUserStore) GetSystemUser() (umodels.User, error) {
	s.t.Helper()
	s.t.Fatal("unexpected userStore.GetSystemUser call")
	return umodels.User{}, nil
}

func (s *notifyUserStore) ResolveContact(*umodels.User, umodels.ContactPolicy) error {
	s.t.Helper()
	s.t.Fatal("unexpected userStore.ResolveContact call")
	return nil
}

func (s *notifyUserStore) UpgradeVisitorToContact(int) error {
	s.t.Helper()
	s.t.Fatal("unexpected userStore.UpgradeVisitorToContact call")
	return nil
}

func newNotifyTestManager(t *testing.T) (*Manager, models.Conversation) {
	t.Helper()
	db := testutil.NewDB(t, "conversation_notify")
	db.MustExec(`INSERT INTO templates (name, type, is_default, body) VALUES ('notify-wrapper', 'email_outgoing', true, $1)`,
		`<article data-site="{{ SiteName }}">{{ template "content" . }}</article>`)
	lo := logf.New(logf.Opts{})
	funcs := template.FuncMap{
		"RootURL":  func() string { return "https://desk.example" },
		"SiteName": func() string { return "Support" },
	}
	renderer, err := tmpl.New(&lo, db, nil, nil, funcs, testutil.NewI18n(t))
	if err != nil {
		t.Fatalf("creating template manager: %v", err)
	}
	store := &notifyUserStore{t: t, recipient: umodels.User{
		ID: 7, FirstName: "Grace", LastName: "Hopper", Email: null.StringFrom("grace@example.com"),
	}}
	conv := models.Conversation{
		ID: 42, UUID: "conversation-42", ReferenceNumber: "0042",
		Contact: models.ConversationContact{FirstName: "Ada", LastName: "Lovelace"},
	}
	return &Manager{template: renderer, userStore: store, lo: &lo}, conv
}

func TestBuildAutomationNotificationPreservesPlainText(t *testing.T) {
	m, conv := newNotifyTestManager(t)
	subject := "Ticket 0042 for Ada Lovelace"
	message := "Please review 0042 with Ada Lovelace."

	notification, emails := m.buildAutomationNotification(subject, message, []int{7}, conv)

	if len(emails) != 1 {
		t.Fatalf("got %d emails, want 1", len(emails))
	}
	if emails[0].Subject != subject {
		t.Errorf("email subject = %q, want %q", emails[0].Subject, subject)
	}
	if !strings.Contains(emails[0].Content, `<p style="white-space: pre-line;">`+message+`</p>`) {
		t.Errorf("email content does not preserve message: %q", emails[0].Content)
	}
	if notification.Title != subject || !notification.Body.Valid || notification.Body.String != message {
		t.Errorf("notification title/body = %q/%v, want %q/%q", notification.Title, notification.Body, subject, message)
	}
	link := `<a href="https://desk.example/inboxes/all/conversation/conversation-42">#0042</a>`
	if !strings.Contains(emails[0].Content, link) {
		t.Errorf("email content = %q, want rendered link %q", emails[0].Content, link)
	}
}
