// Package azblob provides a media.Store implementation backed by Azure Blob Storage.
package azblob

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	azureblob "github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/sas"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/service"
	"github.com/abhinavxd/libredesk/internal/media"
)

const (
	defaultExpiry         = 30 * time.Minute
	delegationKeyLifetime = 7 * 24 * time.Hour
	delegationClockSkew   = 5 * time.Minute
	delegationRefreshLead = 24 * time.Hour
	delegationRetryDelay  = 5 * time.Minute
)

// Opt holds configuration parameters for Azure Blob Storage.
type Opt struct {
	Account       string        `koanf:"account"`
	AccountKey    string        `koanf:"account_key"`
	Container     string        `koanf:"container"`
	ContainerPath string        `koanf:"container_path"`
	Endpoint      string        `koanf:"endpoint"`
	PublicURL     string        `koanf:"public_url"`
	Expiry        time.Duration `koanf:"expiry"`
}

// Client implements media.Store using Azure Blob Storage.
type Client struct {
	client    *azureblob.Client
	opts      Opt
	sharedKey *azureblob.SharedKeyCredential

	delegationMu     sync.Mutex
	delegation       *service.UserDelegationCredential
	delegationExpiry time.Time
	delegationRetry  time.Time
}

var _ media.Store = (*Client)(nil)

// New creates an Azure Blob Storage client. An account key is used when
// configured; otherwise DefaultAzureCredential is used.
func New(opt Opt) (media.Store, error) {
	if opt.Account == "" {
		return nil, fmt.Errorf("azure blob account is required")
	}
	if opt.Container == "" {
		return nil, fmt.Errorf("azure blob container is required")
	}
	if opt.Expiry < time.Second {
		opt.Expiry = defaultExpiry
	}
	if opt.Endpoint == "" {
		opt.Endpoint = fmt.Sprintf("https://%s.blob.core.windows.net", opt.Account)
	}
	opt.Endpoint = strings.TrimRight(opt.Endpoint, "/")
	opt.PublicURL = strings.TrimRight(opt.PublicURL, "/")

	var (
		cl        *azureblob.Client
		sharedKey *azureblob.SharedKeyCredential
		err       error
	)
	if opt.AccountKey != "" {
		sharedKey, err = azureblob.NewSharedKeyCredential(opt.Account, opt.AccountKey)
		if err != nil {
			return nil, fmt.Errorf("creating Azure Blob shared key credential: %w", err)
		}
		cl, err = azureblob.NewClientWithSharedKeyCredential(opt.Endpoint, sharedKey, nil)
	} else {
		if opt.PublicURL == "" && opt.Expiry >= delegationKeyLifetime-delegationClockSkew {
			return nil, fmt.Errorf("azure blob expiry must be less than %s when using managed identity", delegationKeyLifetime-delegationClockSkew)
		}
		credential, credErr := azidentity.NewDefaultAzureCredential(nil)
		if credErr != nil {
			return nil, fmt.Errorf("creating Azure default credential: %w", credErr)
		}
		cl, err = azureblob.NewClient(opt.Endpoint, credential, nil)
	}
	if err != nil {
		return nil, fmt.Errorf("creating Azure Blob client: %w", err)
	}

	c := &Client{
		client:    cl,
		opts:      opt,
		sharedKey: sharedKey,
	}
	if sharedKey == nil && opt.PublicURL == "" {
		if _, _, err := c.getDelegationCredential(context.Background(), time.Now()); err != nil {
			return nil, err
		}
	}
	return c, nil
}

// Put uploads a file to Azure Blob Storage.
func (c *Client) Put(name, contentType string, content io.ReadSeeker) (string, error) {
	_, err := c.client.UploadStream(
		context.Background(),
		c.opts.Container,
		c.makeBlobPath(name),
		content,
		&azureblob.UploadStreamOptions{
			HTTPHeaders: &blob.HTTPHeaders{BlobContentType: &contentType},
		},
	)
	if err != nil {
		return "", fmt.Errorf("azure blob put container=%q blob=%q content_type=%q: %w", c.opts.Container, c.makeBlobPath(name), contentType, err)
	}
	return name, nil
}

// GetURL returns a public URL when configured, or a read-only SAS URL.
func (c *Client) GetURL(name, disposition, fileName string) string {
	if c.opts.PublicURL != "" {
		return c.makeFileURL(name)
	}

	u, err := c.makeSASURL(name, disposition, fileName, time.Now())
	if err != nil {
		// The media.Store interface cannot return URL-generation errors. Returning
		// the unsigned private URL fails closed with Azure rather than exposing data.
		return c.makeFileURL(name)
	}
	return u
}

// GetBlob downloads a blob into memory.
func (c *Client) GetBlob(name string) ([]byte, error) {
	if parsed, err := url.Parse(name); err == nil {
		name = filepath.Base(parsed.Path)
	} else {
		name = filepath.Base(name)
	}

	response, err := c.client.DownloadStream(context.Background(), c.opts.Container, c.makeBlobPath(name), nil)
	if err != nil {
		return nil, fmt.Errorf("azure blob get container=%q blob=%q: %w", c.opts.Container, c.makeBlobPath(name), err)
	}
	reader := response.NewRetryReader(context.Background(), nil)
	defer reader.Close()

	content, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("reading Azure blob container=%q blob=%q: %w", c.opts.Container, c.makeBlobPath(name), err)
	}
	return content, nil
}

// Delete removes a blob.
func (c *Client) Delete(name string) error {
	if _, err := c.client.DeleteBlob(context.Background(), c.opts.Container, c.makeBlobPath(name), nil); err != nil {
		return fmt.Errorf("azure blob delete container=%q blob=%q: %w", c.opts.Container, c.makeBlobPath(name), err)
	}
	return nil
}

// Name returns the storage implementation name.
func (c *Client) Name() string {
	return "azblob"
}

// SignedURLValidator returns nil because Azure validates SAS URLs.
func (c *Client) SignedURLValidator() func(name, sig string, exp int64) bool {
	return nil
}

func (c *Client) makeBlobPath(name string) string {
	prefix := strings.Trim(c.opts.ContainerPath, "/")
	if prefix == "" {
		return name
	}
	return prefix + "/" + name
}

func (c *Client) makeFileURL(name string) string {
	baseURL := c.opts.Endpoint + "/" + c.opts.Container
	if c.opts.PublicURL != "" {
		baseURL = c.opts.PublicURL
	}
	return baseURL + "/" + c.makeBlobPath(name)
}

func (c *Client) makeSASURL(name, disposition, fileName string, now time.Time) (string, error) {
	expiry := now.Add(c.opts.Expiry)
	permissions := sas.BlobPermissions{Read: true}
	values := sas.BlobSignatureValues{
		Protocol:           sas.ProtocolHTTPS,
		ExpiryTime:         expiry,
		Permissions:        permissions.String(),
		ContainerName:      c.opts.Container,
		BlobName:           c.makeBlobPath(name),
		ContentDisposition: mime.FormatMediaType(disposition, map[string]string{"filename": fileName}),
	}
	if strings.HasPrefix(c.opts.Endpoint, "http://") {
		values.Protocol = sas.ProtocolHTTPSandHTTP
	}

	var (
		query sas.QueryParameters
		err   error
	)
	if c.sharedKey != nil {
		query, err = values.SignWithSharedKey(c.sharedKey)
	} else {
		var credential *service.UserDelegationCredential
		credential, expiry, err = c.getDelegationCredential(context.Background(), now)
		values.ExpiryTime = minTime(expiry.Add(-delegationClockSkew), now.Add(c.opts.Expiry))
		if !values.ExpiryTime.After(now) {
			return "", fmt.Errorf("Azure user delegation credential has expired")
		}
		if err == nil {
			query, err = values.SignWithUserDelegation(credential)
		}
	}
	if err != nil {
		return "", fmt.Errorf("signing Azure Blob SAS: %w", err)
	}

	u, err := url.Parse(c.makeFileURL(name))
	if err != nil {
		return "", fmt.Errorf("parsing Azure blob URL: %w", err)
	}
	u.RawQuery = query.Encode()
	return u.String(), nil
}

func (c *Client) getDelegationCredential(ctx context.Context, now time.Time) (*service.UserDelegationCredential, time.Time, error) {
	c.delegationMu.Lock()
	defer c.delegationMu.Unlock()

	requiredExpiry := now.Add(c.opts.Expiry + delegationRefreshLead)
	if c.delegation != nil && c.delegationExpiry.After(requiredExpiry) {
		return c.delegation, c.delegationExpiry, nil
	}
	if c.delegation != nil &&
		c.delegationExpiry.After(now.Add(delegationClockSkew)) &&
		now.Before(c.delegationRetry) {
		return c.delegation, c.delegationExpiry, nil
	}

	start := now.Add(-delegationClockSkew)
	expiry := start.Add(delegationKeyLifetime)
	startValue := start.UTC().Format(time.RFC3339)
	expiryValue := expiry.UTC().Format(time.RFC3339)
	credential, err := c.client.ServiceClient().GetUserDelegationCredential(ctx, service.KeyInfo{
		Start:  &startValue,
		Expiry: &expiryValue,
	}, nil)
	if err != nil {
		if c.delegation != nil && c.delegationExpiry.After(now.Add(delegationClockSkew)) {
			c.delegationRetry = now.Add(delegationRetryDelay)
			return c.delegation, c.delegationExpiry, nil
		}
		return nil, time.Time{}, fmt.Errorf("getting Azure Blob user delegation credential: %w", err)
	}

	c.delegation = credential
	c.delegationExpiry = expiry
	c.delegationRetry = time.Time{}
	return credential, expiry, nil
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
