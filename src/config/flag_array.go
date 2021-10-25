package config

import (
	"strings"
)

type ArrayFlags []string

// String is implemented to fit the flag.Value interface
func (a *ArrayFlags) String() string {
	return strings.Join([]string(*a), ",")
}

// Set is implemented to fit the flag.Value interface
func (a *ArrayFlags) Set(value string) error {
	// Each time we encounter the flag, add an entry to the list,
	// this way the flag can be specified multiple times
	*a = append(*a, value)
	return nil
}
