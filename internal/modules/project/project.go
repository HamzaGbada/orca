// Package project resolves which logical project a resource belongs to.
package project

import "orca/internal/shared/labels"

// Source says how a project was determined, from most to least trusted.
type Source string

const (
	// Label: explicit com.orca.project label.
	Label Source = "label"
	// Compose: inferred from the Docker Compose project label.
	Compose Source = "compose"
)

// Ref is a resource's project; the zero Ref means no project.
type Ref struct {
	Name   string `json:",omitempty"`
	Source Source `json:",omitempty"`
}

// Resolve returns the project of a resource from its labels. An explicit
// com.orca.project label always wins over the inferred Compose project.
func Resolve(l map[string]string) Ref {
	if name := l[labels.Project]; name != "" {
		return Ref{Name: name, Source: Label}
	}
	if name := l[labels.ComposeProject]; name != "" {
		return Ref{Name: name, Source: Compose}
	}
	return Ref{}
}
