package config

import (
	"fmt"
	"regexp"
)

var searchLanguagePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

// ValidateSearch requires SEARCH_LANGUAGE to look like a PostgreSQL text search
// configuration name; whether it exists is checked against the database when the index is built.
func (c Config) ValidateSearch() error {
	if !searchLanguagePattern.MatchString(c.SearchLanguage) {
		return fmt.Errorf("SEARCH_LANGUAGE must be a PostgreSQL text search configuration such as simple or english (got %q)", c.SearchLanguage)
	}

	return nil
}
