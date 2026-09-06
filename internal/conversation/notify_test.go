package conversation

import (
	"errors"
	"html/template"
	"slices"
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
	t          *testing.T
	recipients map[int]umodels.User
	failures   map[int]error
}

var _ userStore = (*notifyUserStore)(nil)

func (s *notifyUserStore) GetAgentCachedOrLoad(id int) (umodels.User, error) {
	s.t.Helper()
	if err := s.failures[id]; err != nil {
		return umodels.User{}, err
	}
	recipient, ok := s.recipients[id]
	if !ok {
		s.t.Fatalf("unexpected recipient ID %d", id)
	}
	return recipient, nil
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
	store := &notifyUserStore{t: t, recipients: map[int]umodels.User{
		7: {ID: 7, FirstName: "Grace", LastName: "Hopper", Email: null.StringFrom("grace@example.com")},
	}}
	conv := models.Conversation{
		ID: 42, UUID: "conversation-42", ReferenceNumber: "0042",
		Contact: models.ConversationContact{FirstName: "Ada", LastName: "Lovelace"},
	}
	return &Manager{template: renderer, userStore: store, lo: &lo}, conv
}

func TestBuildAutomationNotificationKeepsSlotsAfterAgentFetchFailure(t *testing.T) {
	m, conv := newNotifyTestManager(t)
	m.userStore.(*notifyUserStore).failures = map[int]error{8: errors.New("agent unavailable")}
	notification, emails := m.buildAutomationNotification(
		`For {{ .Recipient.FirstName }}`, `Hello {{ .Recipient.FullName }}`, []int{8, 7}, conv)
	if !slices.Equal(notification.RecipientIDs, []int{8, 7}) || len(emails) != 2 {
		t.Fatalf("recipient IDs %v and emails %+v are not aligned", notification.RecipientIDs, emails)
	}
	if len(emails[0].Recipients) != 0 || emails[0].Subject != "" || emails[0].Content != "" {
		t.Errorf("failed recipient has nonempty email: %+v", emails[0])
	}
	if !slices.Equal(emails[1].Recipients, []string{"grace@example.com"}) ||
		emails[1].Subject != "For Grace" || !strings.Contains(emails[1].Content, "Hello Grace Hopper") {
		t.Errorf("later recipient did not receive its rendered email: %+v", emails[1])
	}
}

func TestBuildAutomationNotificationRendersInAppWithoutRecipientEmail(t *testing.T) {
	m, conv := newNotifyTestManager(t)
	store := m.userStore.(*notifyUserStore)
	recipient := store.recipients[7]
	recipient.Email = null.String{}
	store.recipients[7] = recipient
	notification, emails := m.buildAutomationNotification(
		`Ticket {{ .Conversation.ReferenceNumber }}`, `For {{ .Contact.FullName }}`, []int{7}, conv)
	if notification.Title != "Ticket 0042" || !notification.Body.Valid || notification.Body.String != "For Ada Lovelace" {
		t.Errorf("notification was not rendered without recipient email: %+v", notification)
	}
	if len(notification.RecipientIDs) != 1 || notification.RecipientIDs[0] != 7 {
		t.Errorf("notification recipients = %v, want [7]", notification.RecipientIDs)
	}
	if len(emails) != 1 || len(emails[0].Recipients) != 0 || emails[0].Subject != "" || emails[0].Content != "" {
		t.Errorf("emails = %+v, want one empty email slot", emails)
	}
}

func TestBuildAutomationNotificationUsesSharedFunctions(t *testing.T) {
	m, conv := newNotifyTestManager(t)
	notification, emails := m.buildAutomationNotification(
		`{{ SiteName }} at {{ RootURL }}`, `Visit {{ RootURL }} for {{ SiteName }}`, []int{7}, conv)
	wantSubject, wantMessage := "Support at https://desk.example", "Visit https://desk.example for Support"
	if len(emails) != 1 || emails[0].Subject != wantSubject || !strings.Contains(emails[0].Content, wantMessage) {
		t.Errorf("emails = %+v, want shared functions rendered in both rule fields", emails)
	}
	if notification.Title != wantSubject || !notification.Body.Valid || notification.Body.String != wantMessage {
		t.Errorf("notification = %+v, want shared functions rendered in both rule fields", notification)
	}
}

func TestBuildAutomationNotificationDiscardsPartialMessageOnExecutionError(t *testing.T) {
	m, conv := newNotifyTestManager(t)
	message := `For {{ .Contact.FullName }}: {{ .Contact.FullName.Missing }} tail`
	notification, emails := m.buildAutomationNotification(`Ticket {{ .Conversation.ReferenceNumber }}`, message, []int{7}, conv)
	if notification.Title != "Ticket 0042" || !notification.Body.Valid || notification.Body.String != message {
		t.Errorf("notification = %+v, want rendered title and entire original message %q", notification, message)
	}
	if len(emails) != 1 || len(emails[0].Recipients) != 0 || emails[0].Subject != "" || emails[0].Content != "" {
		t.Errorf("emails = %+v, want one empty email slot", emails)
	}
}

func TestBuildAutomationNotificationPreservesMalformedMessage(t *testing.T) {
	m, conv := newNotifyTestManager(t)
	message := `For {{ .Contact.FullName }} {{ if }}broken{{ end }}`
	notification, emails := m.buildAutomationNotification(`Ticket {{ .Conversation.ReferenceNumber }}`, message, []int{7}, conv)
	if notification.Title != "Ticket 0042" || !notification.Body.Valid || notification.Body.String != message {
		t.Errorf("notification = %+v, want rendered subject and raw message", notification)
	}
	if len(emails) != 1 || len(emails[0].Recipients) != 0 || emails[0].Subject != "" || emails[0].Content != "" {
		t.Errorf("emails = %+v, want one empty email slot", emails)
	}
}

func TestBuildAutomationNotificationPreservesMalformedSubject(t *testing.T) {
	m, conv := newNotifyTestManager(t)
	subject := `Ticket {{ .Conversation.ReferenceNumber }} {{ if }}broken{{ end }}`
	notification, emails := m.buildAutomationNotification(subject, `For {{ .Contact.FullName }}`, []int{7}, conv)
	if notification.Title != subject || !notification.Body.Valid || notification.Body.String != "For Ada Lovelace" {
		t.Errorf("notification = %+v, want raw subject and rendered message", notification)
	}
	if len(emails) != 1 || len(emails[0].Recipients) != 0 || emails[0].Subject != "" || emails[0].Content != "" {
		t.Errorf("emails = %+v, want one empty email slot", emails)
	}
}

func TestBuildAutomationNotificationRendersMissingMapKeys(t *testing.T) {
	m, conv := newNotifyTestManager(t)
	field := `{{ .Missing }}|{{ .Missing.Name }}|{{ .Contact.Missing }}`
	want := "<no value>|<no value>|<no value>"
	notification, emails := m.buildAutomationNotification(field, field, []int{7}, conv)
	if len(emails) != 1 {
		t.Fatalf("got %d emails, want 1", len(emails))
	}
	if emails[0].Subject != want || !strings.Contains(emails[0].Content, want) {
		t.Errorf("email = %+v, want missing-key values %q", emails[0], want)
	}
	if notification.Title != want || !notification.Body.Valid || notification.Body.String != want {
		t.Errorf("notification = %+v, want missing-key values %q", notification, want)
	}
}

func TestBuildAutomationNotificationIsolatesRecipients(t *testing.T) {
	m, conv := newNotifyTestManager(t)
	store := m.userStore.(*notifyUserStore)
	store.recipients[8] = umodels.User{ID: 8, FirstName: "Katherine", LastName: "Johnson", Email: null.StringFrom("katherine@example.com")}
	notification, emails := m.buildAutomationNotification(
		`For {{ .Recipient.FullName }} <{{ .Recipient.Email }}>`,
		`Hello {{ .Recipient.FirstName }} | {{ .Recipient.LastName }}`, []int{7, 8}, conv)
	if len(emails) != 2 {
		t.Fatalf("got %d emails, want 2", len(emails))
	}
	for i, id := range []int{7, 8} {
		recipient := store.recipients[id]
		wantSubject := "For " + recipient.FullName() + " <" + recipient.Email.String + ">"
		wantBody := "Hello " + recipient.FirstName + " | " + recipient.LastName
		if emails[i].Subject != wantSubject || !strings.Contains(emails[i].Content, wantBody) {
			t.Errorf("email %d = %+v, want subject %q and body %q", i, emails[i], wantSubject, wantBody)
		}
		if len(emails[i].Recipients) != 1 || emails[i].Recipients[0] != recipient.Email.String {
			t.Errorf("email %d recipients = %v, want %q", i, emails[i].Recipients, recipient.Email.String)
		}
	}
	if notification.Title != "For  <>" || !notification.Body.Valid || notification.Body.String != "Hello  | " {
		t.Errorf("in-app fields contain recipient data: %+v", notification)
	}
}

func TestBuildAutomationNotificationRendersRuleFields(t *testing.T) {
	m, conv := newNotifyTestManager(t)
	subject := `Ticket {{ .Conversation.ReferenceNumber }} for {{ .Contact.FullName }}`
	message := `Please review {{ .Conversation.ReferenceNumber }} with {{ .Contact.FullName }}.`
	wantSubject := "Ticket 0042 for Ada Lovelace"
	wantMessage := "Please review 0042 with Ada Lovelace."

	notification, emails := m.buildAutomationNotification(subject, message, []int{7}, conv)

	if len(emails) != 1 {
		t.Fatalf("got %d emails, want 1", len(emails))
	}
	if emails[0].Subject != wantSubject {
		t.Errorf("email subject = %q, want %q", emails[0].Subject, wantSubject)
	}
	if !strings.Contains(emails[0].Content, `<p style="white-space: pre-line;">`+wantMessage+`</p>`) {
		t.Errorf("email content = %q, want rendered message %q", emails[0].Content, wantMessage)
	}
	if notification.Title != wantSubject {
		t.Errorf("notification title = %q, want %q", notification.Title, wantSubject)
	}
	if !notification.Body.Valid || notification.Body.String != wantMessage {
		t.Errorf("notification body = %v, want %q", notification.Body, wantMessage)
	}
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
