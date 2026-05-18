// Package google implements the Google authorization-code OAuth flow plus
// the verified-secondary-emails pull (People API), which is the core of
// our auto-merge logic.
//
// Two surfaces:
//   * Start() — server-issued state + PKCE challenge, returns authorize URL
//   * Complete() — exchange code → tokens → userinfo + secondaries
package google

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/redis/go-redis/v9"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// Config holds the OAuth client config plus the state-store backing.
type Config struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       []string
	// StateTTL caps how long a state token + PKCE verifier remain valid.
	// Defaults to 5min — matches Google's recommended UX.
	StateTTL time.Duration
}

// DefaultScopes are the scopes we need: openid + profile + email + the
// verified secondaries (people.emails.read isn't an OAuth scope; we get
// secondaries via the userinfo `verified_email` claim and the People API
// scope userinfo.email is sufficient because Google's userinfo endpoint
// embeds the user's verified secondary list when the People API has been
// enabled on the project).
func DefaultScopes() []string {
	return []string{
		oidc.ScopeOpenID,
		"profile",
		"email",
		// People API scope is required to pull the full verified
		// secondaries list via people.connections.list / people.get
		// "emailAddresses" field. Without it we only get the primary.
		"https://www.googleapis.com/auth/user.emails.read",
	}
}

// pendingState is what we stash in Redis between Start and Complete.
type pendingState struct {
	CodeVerifier string `json:"v"`
	ReturnTo     string `json:"r"`
	CreatedAt    int64  `json:"c"`
	Nonce        string `json:"n"`
}

// stateStore is the minimal interface the flow needs to persist state.
// Production wires Redis; tests use an in-memory map.
type stateStore interface {
	put(ctx context.Context, key string, val pendingState, ttl time.Duration) error
	pop(ctx context.Context, key string) (pendingState, error)
}

// Client is the OAuth + OIDC wrapper.
type Client struct {
	oauthConfig *oauth2.Config
	verifier    *oidc.IDTokenVerifier
	store       stateStore
	stateTTL    time.Duration

	// httpClient is used for People API + userinfo calls. Overrideable
	// for tests; defaults to http.DefaultClient.
	httpClient *http.Client
}

// New constructs a Client. The OIDC provider is loaded lazily so unit
// tests can substitute a discovery URL.
func New(ctx context.Context, cfg Config, redisClient *redis.Client) (*Client, error) {
	if cfg.ClientID == "" || cfg.ClientSecret == "" || cfg.RedirectURL == "" {
		return nil, errors.New("google: client id / secret / redirect url required")
	}
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = DefaultScopes()
	}
	if cfg.StateTTL == 0 {
		cfg.StateTTL = 5 * time.Minute
	}
	provider, err := oidc.NewProvider(ctx, "https://accounts.google.com")
	if err != nil {
		return nil, fmt.Errorf("google: oidc discovery: %w", err)
	}
	verifier := provider.Verifier(&oidc.Config{ClientID: cfg.ClientID})
	oc := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURL,
		Endpoint:     google.Endpoint,
		Scopes:       cfg.Scopes,
	}
	var store stateStore
	if redisClient != nil {
		store = &redisStateStore{r: redisClient}
	} else {
		store = newMemoryStateStore()
	}
	return &Client{
		oauthConfig: oc,
		verifier:    verifier,
		store:       store,
		stateTTL:    cfg.StateTTL,
		httpClient:  http.DefaultClient,
	}, nil
}

// NewForTest wires a Client with an explicit OIDC verifier — used so unit
// tests don't need network access to accounts.google.com.
func NewForTest(oc *oauth2.Config, verifier *oidc.IDTokenVerifier, store stateStore, ttl time.Duration, hc *http.Client) *Client {
	if hc == nil {
		hc = http.DefaultClient
	}
	return &Client{
		oauthConfig: oc,
		verifier:    verifier,
		store:       store,
		stateTTL:    ttl,
		httpClient:  hc,
	}
}

// --- Start ---------------------------------------------------------------

// StartResult is what Start hands back to the HTTP handler.
type StartResult struct {
	AuthorizeURL string
	State        string
}

// Start generates state + PKCE, stores them, and returns the authorize URL.
func (c *Client) Start(ctx context.Context, returnTo string) (StartResult, error) {
	state, err := randString(32)
	if err != nil {
		return StartResult{}, err
	}
	verifier, err := randString(32)
	if err != nil {
		return StartResult{}, err
	}
	nonce, err := randString(16)
	if err != nil {
		return StartResult{}, err
	}
	if err := c.store.put(ctx, state, pendingState{
		CodeVerifier: verifier,
		ReturnTo:     returnTo,
		CreatedAt:    time.Now().Unix(),
		Nonce:        nonce,
	}, c.stateTTL); err != nil {
		return StartResult{}, err
	}
	url := c.oauthConfig.AuthCodeURL(state,
		oauth2.AccessTypeOnline,
		oauth2.SetAuthURLParam("code_challenge", s256Challenge(verifier)),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
		oauth2.SetAuthURLParam("nonce", nonce),
		oauth2.SetAuthURLParam("prompt", "select_account"),
	)
	return StartResult{AuthorizeURL: url, State: state}, nil
}

// --- Complete ------------------------------------------------------------

// Identity is the unified result of Complete — the bits we feed into the
// store + auto-merge logic.
type Identity struct {
	Subject              string   // OIDC sub
	Email                string   // primary email
	EmailVerified        bool
	Name                 string
	Picture              string
	HostedDomain         string   // `hd` claim, empty for non-Workspace
	VerifiedSecondaries  []string // verified emails OTHER than primary
	ReturnTo             string   // copied from the pending state
}

// IDClaims is the subset of Google's id_token we care about.
type IDClaims struct {
	Sub           string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
	HD            string `json:"hd"`
	Nonce         string `json:"nonce"`
}

// peopleResponse is the People API "people/me?personFields=emailAddresses"
// shape. We only consume emailAddresses[].value + .metadata.verified.
type peopleResponse struct {
	EmailAddresses []struct {
		Value    string `json:"value"`
		Metadata struct {
			Verified bool `json:"verified"`
			Primary  bool `json:"primary"`
		} `json:"metadata"`
	} `json:"emailAddresses"`
}

// Complete exchanges the auth code, verifies the id_token, fetches People
// API secondaries, and returns the unified Identity. The state token is
// consumed (single-use) inside this method — caller should treat any
// error as terminal and force the user back to Start.
func (c *Client) Complete(ctx context.Context, code, state string) (*Identity, error) {
	if code == "" || state == "" {
		return nil, errors.New("google: code and state required")
	}
	pending, err := c.store.pop(ctx, state)
	if err != nil {
		return nil, fmt.Errorf("google: unknown or expired state: %w", err)
	}
	tok, err := c.oauthConfig.Exchange(ctx, code,
		oauth2.SetAuthURLParam("code_verifier", pending.CodeVerifier),
	)
	if err != nil {
		return nil, fmt.Errorf("google: token exchange: %w", err)
	}
	rawIDToken, ok := tok.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return nil, errors.New("google: response missing id_token")
	}
	idTok, err := c.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("google: id_token verify: %w", err)
	}
	var claims IDClaims
	if err := idTok.Claims(&claims); err != nil {
		return nil, fmt.Errorf("google: parse id_token claims: %w", err)
	}
	if claims.Nonce != "" && claims.Nonce != pending.Nonce {
		return nil, errors.New("google: nonce mismatch")
	}
	id := &Identity{
		Subject:       claims.Sub,
		Email:         strings.ToLower(claims.Email),
		EmailVerified: claims.EmailVerified,
		Name:          claims.Name,
		Picture:       claims.Picture,
		HostedDomain:  claims.HD,
		ReturnTo:      pending.ReturnTo,
	}
	// Pull verified secondaries from the People API. Best-effort: if the
	// scope wasn't granted or the call fails, we still proceed with the
	// primary email — auto-merge will simply not fire for this user.
	if secondaries, err := c.fetchVerifiedSecondaries(ctx, tok); err == nil {
		for _, e := range secondaries {
			lower := strings.ToLower(e)
			if lower != id.Email {
				id.VerifiedSecondaries = append(id.VerifiedSecondaries, lower)
			}
		}
	}
	return id, nil
}

// fetchVerifiedSecondaries calls people.googleapis.com/v1/people/me with
// the user's access token and returns every emailAddress where
// metadata.verified == true.
func (c *Client) fetchVerifiedSecondaries(ctx context.Context, tok *oauth2.Token) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://people.googleapis.com/v1/people/me?personFields=emailAddresses", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("people API %d: %s", resp.StatusCode, string(body))
	}
	var pr peopleResponse
	if err := json.Unmarshal(body, &pr); err != nil {
		return nil, err
	}
	var out []string
	for _, e := range pr.EmailAddresses {
		if e.Metadata.Verified {
			out = append(out, e.Value)
		}
	}
	return out, nil
}

// --- helpers --------------------------------------------------------------

func randString(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func s256Challenge(verifier string) string {
	return base64.RawURLEncoding.EncodeToString(sha256Sum(verifier))
}

// sha256Sum is split out so the test build can avoid the import cycle
// pulling in crypto/sha256 a second time.
func sha256Sum(in string) []byte {
	h := newSHA256()
	h.Write([]byte(in))
	return h.Sum(nil)
}
