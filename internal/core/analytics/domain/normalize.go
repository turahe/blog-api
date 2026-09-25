package domain

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Limits applied while normalising events.
const (
	MaxPathLength     = 512
	MaxReferrerLength = 512
	MaxQueryLength    = 200
	MaxFilterValue    = 100
	MaxFocusSeconds   = 4 * 60 * 60
	MaxPosition       = 1000
	MaxResultCount    = 1_000_000
)

// FilterKeys are the search filters a search event may carry.
var FilterKeys = []string{"category", "tag", "from", "to"}

// NormalizePath returns the canonical form of a site path: it must start with "/", the query
// and fragment (which may carry tokens) are removed, repeated slashes collapse, and a trailing
// slash is dropped.
func NormalizePath(raw string) (string, error) {
	path := strings.TrimSpace(raw)
	if !strings.HasPrefix(path, "/") {
		return "", fmt.Errorf("%w: path must start with /", ErrValidation)
	}

	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}

	if strings.ContainsFunc(path, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) ||
		!utf8.ValidString(path) {
		return "", fmt.Errorf("%w: path contains invalid characters", ErrValidation)
	}

	for strings.Contains(path, "//") {
		path = strings.ReplaceAll(path, "//", "/")
	}

	if len(path) > 1 {
		path = strings.TrimSuffix(path, "/")
	}

	if _, err := url.ParseRequestURI(path); err != nil {
		return "", fmt.Errorf("%w: path is malformed", ErrValidation)
	}

	if len(path) > MaxPathLength {
		return "", fmt.Errorf("%w: path longer than %d bytes", ErrValidation, MaxPathLength)
	}

	return path, nil
}

// NormalizeReferrer keeps the scheme, host, and path of an http(s) referrer and drops the
// rest (credentials, query, fragment). A referrer that is not a usable URL becomes empty.
func NormalizeReferrer(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return ""
	}

	origin := u.Scheme + "://" + strings.ToLower(u.Host)

	path := u.EscapedPath()
	if path == "/" {
		path = ""
	}

	if len(origin)+len(path) > MaxReferrerLength {
		path = ""
	}

	if len(origin) > MaxReferrerLength {
		return ""
	}

	return origin + path
}

// NormalizeQuery lowercases a search query, drops control characters, collapses whitespace,
// and truncates it to MaxQueryLength characters.
func NormalizeQuery(raw string) (string, error) {
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}

		return unicode.ToLower(r)
	}, strings.ToValidUTF8(raw, ""))

	query := strings.Join(strings.Fields(cleaned), " ")
	if query == "" {
		return "", fmt.Errorf("%w: query is required", ErrValidation)
	}

	if utf8.RuneCountInString(query) > MaxQueryLength {
		query = strings.TrimSpace(string([]rune(query)[:MaxQueryLength]))
	}

	return query, nil
}

// NormalizeFilters trims filter values and drops empty ones. Unknown keys are rejected.
func NormalizeFilters(raw map[string]string) (map[string]string, error) {
	filters := make(map[string]string, len(raw))

	for key, value := range raw {
		if !slices.Contains(FilterKeys, key) {
			return nil, fmt.Errorf("%w: unknown filter %q", ErrValidation, key)
		}

		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}

		if utf8.RuneCountInString(value) > MaxFilterValue {
			return nil, fmt.Errorf("%w: filter %s longer than %d characters", ErrValidation, key, MaxFilterValue)
		}

		filters[key] = value
	}

	return filters, nil
}

// NormalizeCountry returns an upper-case ISO 3166-1 alpha-2 code, or empty for anything
// else, including the XX (unknown) and T1 (Tor) values proxies send.
func NormalizeCountry(raw string) string {
	code := strings.ToUpper(strings.TrimSpace(raw))
	if len(code) != 2 || code == "XX" || code[0] < 'A' || code[0] > 'Z' || code[1] < 'A' || code[1] > 'Z' {
		return ""
	}

	return code
}

// ClampFocus bounds cumulative focus time to 0..MaxFocusSeconds.
func ClampFocus(seconds int) int {
	return min(max(seconds, 0), MaxFocusSeconds)
}

var (
	botPattern = regexp.MustCompile(`(?i)bot|crawl|spider|slurp|facebookexternalhit|embedly|preview|headless|` +
		`lighthouse|pingdom|curl|wget|python-|go-http-client|java/|okhttp|axios|node-fetch|httpclient|scrapy|` +
		`phantomjs|puppeteer|playwright|selenium`)
	tabletPattern  = regexp.MustCompile(`(?i)ipad|tablet|kindle|silk|playbook`)
	mobilePattern  = regexp.MustCompile(`(?i)mobi|iphone|ipod|windows phone`)
	androidPattern = regexp.MustCompile(`(?i)android`)
)

// browsers maps user-agent markers to a browser family, checked in order: Chromium-based
// browsers also claim Chrome and Safari.
var browsers = []struct{ marker, name string }{
	{marker: "edg/", name: browserEdge},
	{marker: "edga/", name: browserEdge},
	{marker: "edgios/", name: browserEdge},
	{marker: "opr/", name: browserOpera},
	{marker: "opera", name: browserOpera},
	{marker: "samsungbrowser", name: "samsung"},
	{marker: "firefox", name: browserFirefox},
	{marker: "fxios", name: browserFirefox},
	{marker: "chrome", name: browserChrome},
	{marker: "crios", name: browserChrome},
	{marker: "chromium", name: browserChrome},
	{marker: "safari", name: "safari"},
}

const (
	browserEdge    = "edge"
	browserOpera   = "opera"
	browserFirefox = "firefox"
	browserChrome  = "chrome"
)

// Agent is the coarse classification of a user agent; the string itself is not kept.
type Agent struct {
	Device  Device
	Browser string
	Bot     bool
}

// ClassifyAgent reduces a user agent to a device class and browser family, and flags bots,
// crawlers, and scripted clients. An empty user agent counts as a bot.
func ClassifyAgent(ua string) Agent {
	if strings.TrimSpace(ua) == "" || botPattern.MatchString(ua) {
		return Agent{Device: DeviceDesktop, Browser: "other", Bot: true}
	}

	agent := Agent{Device: DeviceDesktop, Browser: "other"}

	switch {
	case tabletPattern.MatchString(ua), androidPattern.MatchString(ua) && !mobilePattern.MatchString(ua):
		agent.Device = DeviceTablet
	case mobilePattern.MatchString(ua):
		agent.Device = DeviceMobile
	}

	lower := strings.ToLower(ua)
	for _, b := range browsers {
		if strings.Contains(lower, b.marker) {
			agent.Browser = b.name
			break
		}
	}

	return agent
}
