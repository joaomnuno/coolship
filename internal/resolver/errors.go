// Package resolver resolves exact names and UUID pins within their parent scope.
package resolver

import (
	"fmt"
	"strings"
)

type Choice struct {
	UUID string
	Name string
}

type MissingError struct {
	Resource string
	Name     string
	UUID     string
	Scope    string
}

func (e *MissingError) Error() string {
	selector := fmt.Sprintf("named %q", e.Name)
	if e.UUID != "" {
		selector = fmt.Sprintf("with pinned UUID %q", e.UUID)
	}
	return fmt.Sprintf("%s %s not found in %s; check the binding or run coolship link", e.Resource, selector, e.Scope)
}

type AmbiguousError struct {
	Resource string
	Name     string
	UUID     string
	Scope    string
	Choices  []Choice
}

func (e *AmbiguousError) Error() string {
	choices := make([]string, len(e.Choices))
	for i, choice := range e.Choices {
		choices[i] = fmt.Sprintf("%q (%s)", choice.Name, choice.UUID)
	}
	return fmt.Sprintf("%s selector is ambiguous in %s: %s; link an explicit UUID", e.Resource, e.Scope, strings.Join(choices, ", "))
}

// IdentityError prevents a malformed or changed response from redirecting an
// operation to a different resource after scoped selection.
type IdentityError struct {
	Resource     string
	ExpectedUUID string
	ActualUUID   string
	Reason       string
}

func (e *IdentityError) Error() string {
	return fmt.Sprintf("%s identity validation failed: %s (expected UUID %q, received %q)", e.Resource, e.Reason, e.ExpectedUUID, e.ActualUUID)
}
