package main

import (
	"encoding/json"
)

// Filter represents a NIP-01 subscription filter.
type Filter struct {
	IDs     []string            `json:"ids,omitempty"`
	Authors []string            `json:"authors,omitempty"`
	Kinds   []int               `json:"kinds,omitempty"`
	Since   *int64              `json:"since,omitempty"`
	Until   *int64              `json:"until,omitempty"`
	Limit   *int                `json:"limit,omitempty"`
	Tags    map[string][]string `json:"-"` // #e, #p, etc.
}

// MarshalJSON produces JSON with dynamic #<letter> tag keys.
func (f Filter) MarshalJSON() ([]byte, error) {
	m := make(map[string]interface{})
	if len(f.IDs) > 0 {
		m["ids"] = f.IDs
	}
	if len(f.Authors) > 0 {
		m["authors"] = f.Authors
	}
	if len(f.Kinds) > 0 {
		m["kinds"] = f.Kinds
	}
	if f.Since != nil {
		m["since"] = *f.Since
	}
	if f.Until != nil {
		m["until"] = *f.Until
	}
	if f.Limit != nil {
		m["limit"] = *f.Limit
	}
	for k, v := range f.Tags {
		m["#"+k] = v
	}
	return json.Marshal(m)
}

// UnmarshalJSON parses JSON, extracting #<letter> keys into Tags.
func (f *Filter) UnmarshalJSON(data []byte) error {
	// Decode known fields via an alias to avoid recursion.
	type Alias Filter
	var alias Alias
	if err := json.Unmarshal(data, &alias); err != nil {
		return err
	}
	*f = Filter(alias)

	// Decode all fields into a raw map to find tag filters.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	f.Tags = make(map[string][]string)
	for k, v := range raw {
		if len(k) == 2 && k[0] == '#' {
			letter := k[1]
			if (letter >= 'a' && letter <= 'z') || (letter >= 'A' && letter <= 'Z') {
				var values []string
				if err := json.Unmarshal(v, &values); err != nil {
					return err
				}
				f.Tags[string(letter)] = values
			}
		}
	}

	return nil
}

// Matches returns true if the event matches all specified conditions (AND logic).
func (f *Filter) Matches(event *Event) bool {
	if len(f.IDs) > 0 && !contains(f.IDs, event.ID) {
		return false
	}
	if len(f.Authors) > 0 && !contains(f.Authors, event.PubKey) {
		return false
	}
	if len(f.Kinds) > 0 && !containsInt(f.Kinds, event.Kind) {
		return false
	}
	if f.Since != nil && event.CreatedAt < *f.Since {
		return false
	}
	if f.Until != nil && event.CreatedAt > *f.Until {
		return false
	}

	// Tag filters: for each #<letter>, the event must have at least one matching tag value.
	for tagName, filterValues := range f.Tags {
		if !eventHasTagValue(event, tagName, filterValues) {
			return false
		}
	}

	return true
}

// FiltersMatch returns true if the event matches any of the filters (OR logic).
func FiltersMatch(filters []Filter, event *Event) bool {
	for i := range filters {
		if filters[i].Matches(event) {
			return true
		}
	}
	return false
}

// eventHasTagValue checks if the event has a tag with the given name where the
// first value (index 1 of the tag array, i.e., the tag value) is in filterValues.
func eventHasTagValue(event *Event, tagName string, filterValues []string) bool {
	for _, tag := range event.Tags {
		if len(tag) >= 2 && tag[0] == tagName {
			if contains(filterValues, tag[1]) {
				return true
			}
		}
	}
	return false
}

func contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

func containsInt(slice []int, n int) bool {
	for _, v := range slice {
		if v == n {
			return true
		}
	}
	return false
}
