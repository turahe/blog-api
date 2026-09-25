package domain

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Catalogue is the fixed set of known keys.
type Catalogue struct {
	defs  []Definition
	byKey map[string]Definition
}

// NewCatalogue returns a catalogue of defs sorted by key. It panics on a duplicate key or
// a default that fails its own rules, both programming errors.
func NewCatalogue(defs ...Definition) Catalogue {
	c := Catalogue{defs: slices.Clone(defs), byKey: make(map[string]Definition, len(defs))}
	slices.SortFunc(c.defs, func(a, b Definition) int { return strings.Compare(a.Key, b.Key) })

	for _, d := range c.defs {
		if _, dup := c.byKey[d.Key]; dup {
			panic("settings: duplicate key " + d.Key)
		}

		raw, err := json.Marshal(d.Default)
		if err != nil {
			panic(fmt.Sprintf("settings: encode default of %s: %v", d.Key, err))
		}

		if _, v := d.Decode(raw); v != nil {
			panic(fmt.Sprintf("settings: default of %s is invalid: %s", d.Key, v.Message))
		}

		c.byKey[d.Key] = d
	}

	return c
}

// Lookup returns the definition of key.
func (c Catalogue) Lookup(key string) (Definition, bool) {
	d, ok := c.byKey[key]
	return d, ok
}

// Definitions returns every definition sorted by key.
func (c Catalogue) Definitions() []Definition {
	return slices.Clone(c.defs)
}

// Values is a snapshot of every key's effective value, for services that read settings.
type Values struct {
	m map[string]any
}

// NewValues wraps resolved values keyed by setting key.
func NewValues(m map[string]any) Values {
	return Values{m: m}
}

// String returns a string setting, or "" when key is unknown or not a string.
func (v Values) String(key string) string {
	s, _ := v.m[key].(string)
	return s
}

// Int returns an integer setting, or 0.
func (v Values) Int(key string) int64 {
	i, _ := v.m[key].(int64)
	return i
}

// Bool returns a boolean setting, or false.
func (v Values) Bool(key string) bool {
	b, _ := v.m[key].(bool)
	return b
}

// Strings returns a copy of a list setting, or nil.
func (v Values) Strings(key string) []string {
	list, _ := v.m[key].([]string)
	return slices.Clone(list)
}

var (
	localePattern = regexp.MustCompile(`^[a-z]{2,3}(-[A-Z]{2})?$`)
	noMarkup      = regexp.MustCompile(`^[^<>]*$`)
)

// DefaultCatalogue is the product's settings catalogue. Keys that would enforce
// behaviour (comment switches, password policy, session lifetime) are added together
// with the code that enforces them, so an admin never changes a setting that does nothing.
func DefaultCatalogue() Catalogue {
	return NewCatalogue(
		text(CategorySite, "site.name", "Site name shown in titles, emails, and feeds", "Blog", 100),
		text(CategorySite, "site.tagline", "Short description under the site name", "", 200),
		Definition{
			Key: "site.default_locale", Category: CategorySite, Type: TypeString, Sensitivity: PublicSafe,
			Description: "Default content locale (BCP 47, e.g. en or pt-BR)", Default: "en",
			MaxLength: 10, Pattern: localePattern,
		},
		Definition{
			Key: "site.timezone", Category: CategorySite, Type: TypeString, Sensitivity: PublicSafe,
			Description: "IANA time zone for displayed dates", Default: "UTC", MaxLength: 64, Check: checkTimezone,
		},
		link(CategorySite, "site.public_url", "Public site URL used in links"),
		link(CategorySite, "site.canonical_base_url", "Base URL for canonical links; empty uses site.public_url"),

		Definition{
			Key: "media.default_transform_quality", Category: CategoryMedia, Type: TypeInteger, Sensitivity: PublicSafe,
			Description: "Default quality (1-100) for resized images", Default: int64(80), Min: 1, Max: 100,
		},
		Definition{
			Key: "media.default_transform_format", Category: CategoryMedia, Type: TypeString, Sensitivity: PublicSafe,
			Description: "Output format when a transform or preset names none; original keeps the source format",
			Default:     "original", Enum: []string{"original", "webp", "avif", "jpeg", "png"},
		},
		Definition{
			Key: "media.variants", Category: CategoryMedia, Type: TypeStringList, Sensitivity: PublicSafe,
			Description: "Image presets returned on media responses, as name:width or name:width:format (width 16-4096)",
			Default:     []string{"thumbnail:320:webp", "card:640:webp", "hero:1280:webp"},
			MaxItems:    10, MaxLength: 48, Pattern: variantPattern, Check: checkVariants,
		},

		flag(CategoryAnalytics, "analytics.enabled", "Collect first-party analytics events", true),
		flag(CategoryAnalytics, "analytics.consent_required", "Collect analytics only after visitor consent", true),
		Definition{
			Key: "analytics.raw_retention_days", Category: CategoryAnalytics, Type: TypeInteger, Sensitivity: AdminOnly,
			Description: "Days to keep raw analytics events before aggregation", Default: int64(90), Min: 1, Max: 730,
		},
		Definition{
			Key: "analytics.rollup_day_retention_months", Category: CategoryAnalytics, Type: TypeInteger, Sensitivity: AdminOnly,
			Description: "Months to keep daily analytics rollups (0 keeps them forever); weekly and monthly ones are kept forever",
			Default:     int64(25), Min: 0, Max: 120,
		},

		flag(CategorySecurity, "security.registration_enabled",
			"Let visitors create accounts at POST /api/v1/auth/register (verified by email)", false),

		Definition{
			Key: "notifications.moderation_recipient_roles", Category: CategoryNotifications, Type: TypeStringList,
			Sensitivity: AdminOnly, Description: "Roles notified about comments awaiting moderation",
			Default: []string{"admin", "moderator"}, Enum: []string{"admin", "editor", "moderator"}, MaxItems: 3,
		},
		Definition{
			Key: "notifications.digest_cadence", Category: CategoryNotifications, Type: TypeString, Sensitivity: AdminOnly,
			Description: "How often digest emails are sent", Default: "off", Enum: []string{"off", "daily", "weekly"},
		},

		Definition{
			Key: "seo.title_template", Category: CategorySEO, Type: TypeString, Sensitivity: PublicSafe,
			Description: "Page title template; {title} is the page title and {site} the site name",
			Default:     "{title} | {site}", MaxLength: 120, Pattern: noMarkup, Check: checkTitleTemplate,
		},
		Definition{
			Key: "seo.default_description", Category: CategorySEO, Type: TypeString, Sensitivity: PublicSafe,
			Description: "Meta description when a page has none", Default: "", MaxLength: 300, Pattern: noMarkup,
		},
		link(CategorySEO, "seo.default_share_image_url", "Social share image when a page has none"),
		Definition{
			Key: "seo.default_twitter_card", Category: CategorySEO, Type: TypeString, Sensitivity: PublicSafe,
			Description: "Twitter card type when a post sets none", Default: "summary_large_image",
			Enum: []string{"summary", "summary_large_image"},
		},
		Definition{
			Key: "seo.canonical_allowed_hosts", Category: CategorySEO, Type: TypeStringList, Sensitivity: PublicSafe,
			Description: "Extra hosts a post's canonical_url or og_url may point at; the canonical base URL's host is always allowed",
			Default:     []string{}, MaxItems: 20, MaxLength: 253, Pattern: hostPattern,
		},
	)
}

// hostPattern matches a lowercase DNS host name.
var hostPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$`)

func text(category Category, key, description, def string, maxLength int) Definition {
	return Definition{
		Key: key, Category: category, Type: TypeString, Sensitivity: PublicSafe,
		Description: description, Default: def, MaxLength: maxLength, Pattern: noMarkup,
	}
}

func flag(category Category, key, description string, def bool) Definition {
	return Definition{
		Key: key, Category: category, Type: TypeBoolean, Sensitivity: PublicSafe,
		Description: description, Default: def,
	}
}

func link(category Category, key, description string) Definition {
	return Definition{
		Key: key, Category: category, Type: TypeString, Sensitivity: PublicSafe,
		Description: description, Default: "", MaxLength: 2048, Check: checkURL,
	}
}

func checkTimezone(value any) (string, string) {
	name, _ := value.(string)
	if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, "..") {
		return ReasonFormat, "value must be an IANA time zone such as Europe/Berlin"
	}

	if _, err := time.LoadLocation(name); err != nil {
		return ReasonFormat, "value must be an IANA time zone such as Europe/Berlin"
	}

	return "", ""
}

func checkURL(value any) (string, string) {
	raw, _ := value.(string)
	if raw == "" {
		return "", ""
	}

	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
		return ReasonFormat, "value must be an absolute http(s) URL without credentials"
	}

	return "", ""
}

// variantPattern matches a media preset: name:width or name:width:format.
var variantPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}:[0-9]{2,4}(:(webp|avif|jpeg|png))?$`)

// Variant width bounds, matching the media domain.
const (
	minVariantWidth = 16
	maxVariantWidth = 4096
)

func checkVariants(value any) (string, string) {
	list, _ := value.([]string)
	names := make(map[string]struct{}, len(list))

	for _, spec := range list {
		name, rest, _ := strings.Cut(spec, ":")
		widthText, _, _ := strings.Cut(rest, ":")

		width, err := strconv.Atoi(widthText)
		if err != nil || width < minVariantWidth || width > maxVariantWidth {
			return ReasonOutOfRange, fmt.Sprintf("preset %q width must be between %d and %d", spec, minVariantWidth, maxVariantWidth)
		}

		if _, dup := names[name]; dup {
			return ReasonNotAllowed, fmt.Sprintf("preset name %q is repeated", name)
		}

		names[name] = struct{}{}
	}

	return "", ""
}

func checkTitleTemplate(value any) (string, string) {
	if s, _ := value.(string); !strings.Contains(s, "{title}") {
		return ReasonFormat, "value must contain {title}"
	}

	return "", ""
}
