package ticketfairy

import (
	"context"
	"net/http"
	"os"
	"strings"
	"time"
)

// Version is this module's version, sent in the User-Agent header.
const Version = "0.1.1"

const (
	// PublicBaseURL is the consumer site, which serves the public event listing.
	PublicBaseURL = "https://www.ticketfairy.com"
	// OrganiserBaseURL is the dashboard's domain, which serves the organiser API.
	OrganiserBaseURL = "https://www.theticketfairy.com"

	jsonAPI = "application/vnd.api+json"
)

// Client talks to the Ticket Fairy public and organiser APIs. It is safe for
// concurrent use.
type Client struct {
	// Events reads public event listings and works with your own events.
	Events *EventsService
	// SetupCopy copies another event's setup into an event.
	SetupCopy *SetupCopyService

	token            string
	publicBaseURL    string
	organiserBaseURL string
	transport        *transport
}

// Option configures a Client.
type Option func(*Client)

// WithToken sets the personal access token. Without one, only the public
// event listing works. When no token is given, New reads the
// TICKETFAIRY_TOKEN environment variable.
func WithToken(token string) Option {
	return func(c *Client) { c.token = token }
}

// WithPublicBaseURL replaces the consumer site's address.
func WithPublicBaseURL(url string) Option {
	return func(c *Client) { c.publicBaseURL = strings.TrimRight(url, "/") }
}

// WithOrganiserBaseURL replaces the organiser API's address.
func WithOrganiserBaseURL(url string) Option {
	return func(c *Client) { c.organiserBaseURL = strings.TrimRight(url, "/") }
}

// WithHTTPClient sets the HTTP client. The client's redirect policy is
// replaced so that redirects are never followed: following one would send the
// token to another address. Its Timeout limits each attempt.
func WithHTTPClient(httpClient *http.Client) Option {
	return func(c *Client) { c.transport.httpClient = httpClient }
}

// WithMaxRetries sets how many times a request that is safe to repeat is
// retried. The default is 2.
func WithMaxRetries(retries int) Option {
	return func(c *Client) { c.transport.maxRetries = retries }
}

// WithMaxRetryWait caps the wait before any one retry, including a
// Retry-After the server asks for. The default is one minute.
func WithMaxRetryWait(wait time.Duration) Option {
	return func(c *Client) { c.transport.maxRetryWait = wait }
}

// WithUserAgent adds your product to the User-Agent header, ahead of this
// module's own name and version.
func WithUserAgent(product string) Option {
	return func(c *Client) {
		if product != "" {
			c.transport.userAgent = product + " " + defaultUserAgent
		}
	}
}

// withSleep replaces the wait between attempts and while following a setup
// copy. Tests use it to run without real delays.
func withSleep(sleep func(context.Context, time.Duration) error) Option {
	return func(c *Client) { c.transport.sleep = sleep }
}

const defaultUserAgent = "ticketfairy-go/" + Version

// New returns a Client.
func New(options ...Option) *Client {
	c := &Client{
		publicBaseURL:    PublicBaseURL,
		organiserBaseURL: OrganiserBaseURL,
		transport: &transport{
			httpClient:   &http.Client{Timeout: 30 * time.Second},
			userAgent:    defaultUserAgent,
			maxRetries:   2,
			maxRetryWait: time.Minute,
			sleep:        sleepContext,
		},
	}
	for _, option := range options {
		option(c)
	}
	if c.token == "" {
		c.token = os.Getenv("TICKETFAIRY_TOKEN")
	}
	c.transport.prepare()
	c.Events = &EventsService{client: c}
	c.SetupCopy = &SetupCopyService{client: c}
	return c
}

// Authenticated reports whether the client has a personal access token.
func (c *Client) Authenticated() bool {
	return c.token != ""
}

// String describes the client without its token.
func (c *Client) String() string {
	authenticated := "false"
	if c.Authenticated() {
		authenticated = "true"
	}
	return "ticketfairy.Client{organiser: " + c.organiserBaseURL + ", authenticated: " + authenticated + "}"
}

// GoString keeps the token out of %#v as well.
func (c *Client) GoString() string {
	return c.String()
}

func (c *Client) public(ctx context.Context, r request) (*response, error) {
	r.url = c.publicBaseURL + r.path
	return c.transport.do(ctx, r)
}

func (c *Client) organiser(ctx context.Context, r request) (*response, error) {
	if c.token == "" {
		return nil, &Error{
			Kind:    ErrAuthentication,
			Message: "This needs a personal access token. Create one in your Ticket Fairy account settings and pass it with ticketfairy.WithToken, or set TICKETFAIRY_TOKEN.",
		}
	}
	if r.headers == nil {
		r.headers = map[string]string{}
	}
	r.headers["Authorization"] = "Bearer " + c.token
	if _, ok := r.headers["Accept"]; !ok {
		r.headers["Accept"] = jsonAPI
	}
	r.url = c.organiserBaseURL + r.path
	return c.transport.do(ctx, r)
}
