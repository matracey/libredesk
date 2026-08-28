package azblob

import (
	"encoding/base64"
	"mime"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNewValidation(t *testing.T) {
	accountKey := base64.StdEncoding.EncodeToString([]byte("test-account-key"))

	tests := []struct {
		name    string
		opt     Opt
		wantErr string
	}{
		{
			name:    "account is required",
			opt:     Opt{Container: "media", AccountKey: accountKey},
			wantErr: "azure blob account is required",
		},
		{
			name:    "container is required",
			opt:     Opt{Account: "example", AccountKey: accountKey},
			wantErr: "azure blob container is required",
		},
		{
			name: "managed identity expiry must fit delegation key",
			opt: Opt{
				Account:   "example",
				Container: "media",
				Expiry:    delegationKeyLifetime,
			},
			wantErr: "azure blob expiry must be less than",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.opt)
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestNewDefaultsAndNormalization(t *testing.T) {
	client := newTestClient(t, Opt{
		Account:   "example",
		Container: "media",
	})
	require.Equal(t, "https://example.blob.core.windows.net", client.opts.Endpoint)
	require.Equal(t, defaultExpiry, client.opts.Expiry)

	client = newTestClient(t, Opt{
		Account:   "example",
		Container: "media",
		Endpoint:  "http://127.0.0.1:10000/example///",
		PublicURL: "https://cdn.example.com/media/",
	})
	require.Equal(t, "http://127.0.0.1:10000/example", client.opts.Endpoint)
	require.Equal(t, "https://cdn.example.com/media", client.opts.PublicURL)
}

func TestMakeBlobPath(t *testing.T) {
	tests := []struct {
		name          string
		containerPath string
		want          string
	}{
		{name: "empty prefix", want: "blob-id"},
		{name: "plain prefix", containerPath: "uploads/media", want: "uploads/media/blob-id"},
		{name: "surrounding slashes", containerPath: "/uploads/media/", want: "uploads/media/blob-id"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &Client{opts: Opt{ContainerPath: tt.containerPath}}
			require.Equal(t, tt.want, client.makeBlobPath("blob-id"))
		})
	}
}

func TestGetURLWithPublicURL(t *testing.T) {
	client := newTestClient(t, Opt{
		Account:       "example",
		Container:     "media",
		ContainerPath: "uploads",
		PublicURL:     "https://cdn.example.com/media/",
	})

	require.Equal(t, "https://cdn.example.com/media/uploads/blob-id", client.GetURL("blob-id", "inline", "photo.png"))
}

func TestNewWithPublicURLDoesNotRequestDelegationKey(t *testing.T) {
	store, err := New(Opt{
		Account:   "example",
		Container: "media",
		Endpoint:  "http://127.0.0.1:1",
		PublicURL: "https://cdn.example.com/media",
		Expiry:    delegationKeyLifetime,
	})
	require.NoError(t, err)
	require.IsType(t, &Client{}, store)
}

func TestMakeSASURL(t *testing.T) {
	client := newTestClient(t, Opt{
		Account:       "example",
		Container:     "media",
		ContainerPath: "uploads",
		Expiry:        45 * time.Minute,
	})
	now := time.Date(2026, time.August, 27, 12, 0, 0, 0, time.UTC)

	rawURL, err := client.makeSASURL("blob-id", "attachment", "report final.pdf", now)
	require.NoError(t, err)

	parsed, err := url.Parse(rawURL)
	require.NoError(t, err)
	require.Equal(t, "/media/uploads/blob-id", parsed.Path)

	query := parsed.Query()
	require.NotEmpty(t, query.Get("sig"))
	require.Equal(t, "r", query.Get("sp"))
	require.Equal(t, "b", query.Get("sr"))
	require.Equal(t, "https", query.Get("spr"))

	expiry, err := time.Parse(time.RFC3339, query.Get("se"))
	require.NoError(t, err)
	require.Equal(t, now.Add(45*time.Minute), expiry)

	disposition, params, err := mime.ParseMediaType(query.Get("rscd"))
	require.NoError(t, err)
	require.Equal(t, "attachment", disposition)
	require.Equal(t, "report final.pdf", params["filename"])
}

func TestMakeSASURLAllowsHTTPForCustomEndpoint(t *testing.T) {
	client := newTestClient(t, Opt{
		Account:   "devstoreaccount1",
		Container: "media",
		Endpoint:  "http://127.0.0.1:10000/devstoreaccount1",
	})

	rawURL, err := client.makeSASURL("blob-id", "inline", "photo.png", time.Now())
	require.NoError(t, err)

	parsed, err := url.Parse(rawURL)
	require.NoError(t, err)
	require.Equal(t, "https,http", parsed.Query().Get("spr"))
}

func TestStoreMetadata(t *testing.T) {
	client := newTestClient(t, Opt{
		Account:   "example",
		Container: "media",
	})

	require.Equal(t, "azblob", client.Name())
	require.Nil(t, client.SignedURLValidator())
}

func newTestClient(t *testing.T, opt Opt) *Client {
	t.Helper()
	opt.AccountKey = base64.StdEncoding.EncodeToString([]byte("test-account-key"))

	store, err := New(opt)
	require.NoError(t, err)
	client, ok := store.(*Client)
	require.True(t, ok)
	return client
}
