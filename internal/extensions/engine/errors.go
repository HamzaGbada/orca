package engine

import "errors"

// ErrUnavailable is returned (wrapped) when the engine daemon cannot be
// reached: socket missing, permission denied, daemon stopped, or timeout.
var ErrUnavailable = errors.New("container engine unavailable")

// ErrNotFound is returned (wrapped) when a resource does not exist, for
// example because it was removed between listing and inspecting it.
var ErrNotFound = errors.New("resource not found")
