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
	"time"

	azureblob "github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/sas"
	"github.com/abhinavxd/libredesk/internal/media"
)

const defaultExpiry = 30 * time.Minute

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
}

var _ media.Store = (*Client)(nil)

// New creates an Azure Blob Storage client using an account key.
func New(opt Opt) (media.Store, error) {
	if opt.Account == "" {
		return nil, fmt.Errorf("azure blob account is required")
	}
	if opt.Container == "" {
		return nil, fmt.Errorf("azure blob container is required")
	}
	if opt.AccountKey == "" {
		return nil, fmt.Errorf("azure blob account key is required")
	}
	if opt.Expiry < time.Second {
		opt.Expiry = defaultExpiry
	}
	if opt.Endpoint == "" {
		opt.Endpoint = fmt.Sprintf("https://%s.blob.core.windows.net", opt.Account)
	}
	opt.Endpoint = strings.TrimRight(opt.Endpoint, "/")
	opt.PublicURL = strings.TrimRight(opt.PublicURL, "/")

	sharedKey, err := azureblob.NewSharedKeyCredential(opt.Account, opt.AccountKey)
	if err != nil {
		return nil, fmt.Errorf("creating Azure Blob shared key credential: %w", err)
	}
	cl, err := azureblob.NewClientWithSharedKeyCredential(opt.Endpoint, sharedKey, nil)
	if err != nil {
		return nil, fmt.Errorf("creating Azure Blob client: %w", err)
	}

	return &Client{
		client:    cl,
		opts:      opt,
		sharedKey: sharedKey,
	}, nil
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

	query, err := values.SignWithSharedKey(c.sharedKey)
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
