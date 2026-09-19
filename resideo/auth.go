package resideo

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/unixpickle/tcc/thermostat"
)

const (
	authOrigin  = "https://lyricprod.b2clogin.com"
	authBase    = authOrigin + "/lyricprod.onmicrosoft.com"
	chilBase    = "https://lyric.alarmnet.com"
	titanBase   = "https://api.resideo.com/consumerapi"
	clientID    = "d7baddb4-d9f3-4575-af28-f1b04a7883f2"
	redirectURI = "com.honeywell.acs.lyric.enterprise://oauth2redirect"
	policy      = "b2c_1a_signin_mob_hh"
	scope       = "https://lyricprod.onmicrosoft.com/CHILAPIService/user_impersonation profile email openid offline_access"
	// Public gateway key bundled with the Resideo app, not an account credential.
	subscriptionKey = "8c9485374bab4b1aa4b3555ac2f032c4"
	userAgent       = "Lyric/6.21.0(Pixel 8;Android 15)"
	webUserAgent    = "Mozilla/5.0 (Linux; Android 15; Pixel 8; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/128.0.6613.146 Mobile Safari/537.36"
)

var errLoginRejected = errors.New("Resideo login rejected; verify credentials or complete interactive sign-in in the Resideo app")
var settingsPattern = regexp.MustCompile(`\bvar\s+SETTINGS\s*=\s*`)

type authState struct {
	AccessToken             string      `json:"access_token"`
	RefreshToken            string      `json:"refresh_token"`
	ExpiresIn               json.Number `json:"expires_in"`
	expires                 time.Time
	policy, verifier, nonce string
}

type apiClient struct {
	http               *http.Client
	username, password string
	authMu             sync.Mutex
	auth               authState
	lastAuthFailure    time.Time
	authErr            error
}

func newAPIClient(username, password string) *apiClient {
	jar, _ := cookiejar.New(nil)
	return &apiClient{
		username: username, password: password,
		http: &http.Client{Jar: jar, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}
}

// token serializes refreshes and lets concurrent 401s reuse the new token.
func (c *apiClient) token(rejected string) (string, error) {
	c.authMu.Lock()
	defer c.authMu.Unlock()
	if c.auth.AccessToken != "" && time.Now().Before(c.auth.expires) && c.auth.AccessToken != rejected {
		return c.auth.AccessToken, nil
	}
	if c.authErr != nil && time.Since(c.lastAuthFailure) < time.Minute {
		return "", c.authErr
	}
	var next authState
	var err error
	if c.auth.RefreshToken != "" {
		next, err = c.exchange(c.auth, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {c.auth.RefreshToken}})
		if errors.Is(err, thermostat.ErrUnauthorized) {
			next, err = c.signIn()
		}
	} else {
		next, err = c.signIn()
	}
	if err != nil {
		c.lastAuthFailure, c.authErr = time.Now(), err
		return "", err
	}
	c.auth, c.authErr = next, nil
	return next.AccessToken, nil
}

func randomString(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func (c *apiClient) signIn() (authState, error) {
	state := randomString(24)
	result := authState{policy: policy, verifier: randomString(48), nonce: randomString(24)}
	challenge := sha256.Sum256([]byte(result.verifier))
	query := url.Values{
		"client_id": {clientID}, "redirect_uri": {redirectURI}, "response_type": {"code"},
		"scope": {scope}, "p": {policy}, "code_challenge_method": {"S256"},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(challenge[:])}, "state": {state},
		"ui_locales": {"en-US"}, "country": {"US"}, "User-Agent": {userAgent}, "enableContractor": {"false"},
	}
	authorizeURL := authBase + "/oauth2/v2.0/authorize?" + query.Encode()
	pageURL := authorizeURL
	var data []byte
	for i := 0; i < 8; i++ {
		if err := validateAuthURL(pageURL); err != nil {
			return authState{}, err
		}
		status, headers, body, err := c.request(http.MethodGet, pageURL, nil, nil, webUserAgent)
		if err != nil {
			return authState{}, err
		}
		if status == http.StatusOK {
			data = body
			break
		}
		if !isRedirect(status) {
			return authState{}, fmt.Errorf("Resideo authorization page: HTTP %d", status)
		}
		pageURL, err = resolveRedirect(pageURL, headers.Get("Location"))
		if err != nil {
			return authState{}, err
		}
	}
	settings, err := parseSettings(data)
	if err != nil {
		return authState{}, err
	}
	result.policy = settings.Hosts.Policy
	tenant := authOrigin + settings.Hosts.Tenant
	query = url.Values{"tx": {settings.TransID}, "p": {result.policy}}
	headers := http.Header{
		"Accept": {"application/json"}, "Origin": {authOrigin}, "Referer": {pageURL},
		"X-Csrf-Token": {settings.CSRF}, "X-Requested-With": {"XMLHttpRequest"},
		"Content-Type": {"application/x-www-form-urlencoded"},
	}
	if settings.SendPageViewID && settings.PageViewID != "" {
		headers.Set("x-ms-cpim-pageviewid", settings.PageViewID)
	}
	form := url.Values{"request_type": {"RESPONSE"}, "signInName": {c.username}, "password": {c.password}}
	status, _, body, err := c.request(http.MethodPost, tenant+"/SelfAsserted?"+query.Encode(), []byte(form.Encode()), headers, webUserAgent)
	if err != nil {
		return authState{}, err
	}
	var login struct {
		Status json.Number `json:"status"`
	}
	if status != http.StatusOK || json.Unmarshal(body, &login) != nil || login.Status.String() != "200" {
		return authState{}, errLoginRejected
	}
	query.Set("csrf_token", settings.CSRF)
	nextURL := tenant + "/api/CombinedSigninAndSignup/confirmed?" + query.Encode()
	for i := 0; i < 8; i++ {
		if err := validateAuthURL(nextURL); err != nil {
			return authState{}, err
		}
		status, headers, _, err := c.request(http.MethodGet, nextURL, nil, nil, webUserAgent)
		if err != nil {
			return authState{}, err
		}
		if !isRedirect(status) {
			return authState{}, errLoginRejected
		}
		nextURL, err = resolveRedirect(nextURL, headers.Get("Location"))
		if err != nil {
			return authState{}, err
		}
		if strings.HasPrefix(nextURL, "com.honeywell.") {
			code, err := callbackCode(nextURL, state)
			if err != nil {
				return authState{}, err
			}
			return c.exchange(result, url.Values{"grant_type": {"authorization_code"}, "code": {code}})
		}
	}
	return authState{}, errors.New("Resideo sign-in did not reach authorization callback")
}

type loginSettings struct {
	API            string `json:"api"`
	CSRF           string `json:"csrf"`
	TransID        string `json:"transId"`
	PageViewID     string `json:"pageViewId"`
	SendPageViewID bool   `json:"isPageViewIdSentWithHeader"`
	Hosts          struct {
		Policy string `json:"policy"`
		Tenant string `json:"tenant"`
	} `json:"hosts"`
}

func parseSettings(data []byte) (loginSettings, error) {
	var s loginSettings
	match := settingsPattern.FindIndex(data)
	if match == nil {
		return s, errors.New("Resideo login page has no B2C settings; interactive sign-in may be required")
	}
	if err := json.NewDecoder(bytes.NewReader(data[match[1]:])).Decode(&s); err != nil {
		return s, errors.New("invalid Resideo login page settings")
	}
	if s.API != "CombinedSigninAndSignup" || s.CSRF == "" || s.TransID == "" || s.Hosts.Policy == "" || !strings.HasPrefix(s.Hosts.Tenant, "/lyricprod.onmicrosoft.com/") || strings.ContainsAny(s.Hosts.Tenant, "?#") {
		return s, errors.New("unexpected Resideo login page settings")
	}
	return s, validateAuthURL(authOrigin + s.Hosts.Tenant)
}

func validateAuthURL(value string) error {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.Host != "lyricprod.b2clogin.com" || u.User != nil {
		return errors.New("unexpected Resideo identity-provider URL")
	}
	return nil
}

func resolveRedirect(base, location string) (string, error) {
	b, err := url.Parse(base)
	if err != nil || location == "" {
		return "", errors.New("invalid Resideo redirect")
	}
	u, err := url.Parse(location)
	if err != nil {
		return "", errors.New("invalid Resideo redirect")
	}
	return b.ResolveReference(u).String(), nil
}

func callbackCode(value, state string) (string, error) {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "com.honeywell.acs.lyric.enterprise" || u.Host != "oauth2redirect" || (u.Path != "" && u.Path != "/") || u.User != nil || u.Fragment != "" {
		return "", errors.New("unexpected Resideo authorization callback")
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(q["state"]) != 1 || subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(state)) != 1 {
		return "", errors.New("Resideo authorization state mismatch")
	}
	if q.Has("error") || len(q["code"]) != 1 || q.Get("code") == "" {
		return "", errLoginRejected
	}
	return q.Get("code"), nil
}

func (c *apiClient) exchange(previous authState, form url.Values) (authState, error) {
	form.Set("client_id", clientID)
	form.Set("redirect_uri", redirectURI)
	form.Set("scope", scope)
	form.Set("code_verifier", previous.verifier)
	form.Set("nonce", previous.nonce)
	endpoint := authBase + "/oauth2/v2.0/token?" + url.Values{"p": {previous.policy}}.Encode()
	status, _, body, err := c.request(http.MethodPost, endpoint, []byte(form.Encode()), http.Header{"Content-Type": {"application/x-www-form-urlencoded"}}, userAgent)
	if err != nil {
		return authState{}, err
	}
	if status == http.StatusBadRequest || status == http.StatusUnauthorized {
		return authState{}, fmt.Errorf("Resideo token exchange: %w", thermostat.ErrUnauthorized)
	}
	var result authState
	if status != http.StatusOK || json.Unmarshal(body, &result) != nil || result.AccessToken == "" {
		return authState{}, fmt.Errorf("Resideo token exchange failed (HTTP %d)", status)
	}
	seconds, err := result.ExpiresIn.Int64()
	if err != nil || seconds <= 0 {
		return authState{}, errors.New("Resideo token response missing expiry")
	}
	result.expires = time.Now().Add(time.Duration(seconds)*time.Second - min(30*time.Second, time.Duration(seconds)*time.Second/10))
	result.policy, result.verifier, result.nonce = previous.policy, previous.verifier, previous.nonce
	if result.RefreshToken == "" {
		result.RefreshToken = previous.RefreshToken
	}
	return result, nil
}

func isRedirect(status int) bool {
	return status == 301 || status == 302 || status == 303 || status == 307 || status == 308
}

// request deliberately excludes response bodies and URL queries from errors:
// identity-provider responses can contain credentials, tokens, or auth codes.
func (c *apiClient) request(method, endpoint string, body []byte, headers http.Header, agent string) (int, http.Header, []byte, error) {
	req, err := http.NewRequest(method, endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, nil, nil, errors.New("invalid Resideo request")
	}
	if headers != nil {
		req.Header = headers.Clone()
	}
	req.Header.Set("User-Agent", agent)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("Resideo %s %s: network request failed", method, req.URL.Path)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return 0, nil, nil, errors.New("reading Resideo response failed")
	}
	return resp.StatusCode, resp.Header, data, nil
}

func (c *apiClient) api(method, base, path string, input, output any) error {
	var body []byte
	var err error
	if input != nil {
		body, err = json.Marshal(input)
		if err != nil {
			return err
		}
	}
	token, err := c.token("")
	if err != nil {
		return err
	}
	for attempt := 0; attempt < 2; attempt++ {
		uuid := make([]byte, 16)
		if _, err := rand.Read(uuid); err != nil {
			return err
		}
		uuid[6], uuid[8] = uuid[6]&0x0f|0x40, uuid[8]&0x3f|0x80
		headers := http.Header{
			"Authorization": {"Bearer " + token}, "Ocp-Apim-Subscription-Key": {subscriptionKey},
			"Content-Type": {"application/json; charset=utf-8"}, "Appver": {"6.21.0"},
			"Mobileclienttime": {time.Now().UTC().Format("2006-01-02T15:04:05")},
			"Activityid":       {fmt.Sprintf("%x-%x-%x-%x-%x", uuid[:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:])},
		}
		status, _, data, err := c.request(method, base+path, body, headers, userAgent)
		if err != nil {
			return err
		}
		if status == http.StatusUnauthorized && attempt == 0 {
			token, err = c.token(token)
			if err != nil {
				return err
			}
			continue
		}
		if status == http.StatusUnauthorized {
			return thermostat.ErrUnauthorized
		}
		if status == http.StatusNotFound {
			return thermostat.ErrNotFound
		}
		if status < 200 || status >= 300 {
			return fmt.Errorf("Resideo %s %s: HTTP %d", method, path, status)
		}
		if output != nil && json.Unmarshal(data, output) != nil {
			return errors.New("invalid Resideo API response")
		}
		return nil
	}
	return thermostat.ErrUnauthorized
}
