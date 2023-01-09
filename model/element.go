package model

import "fmt"

// Vars is a map of variables.  The variables must be primative go types.
type Vars map[string]any

// Get validates that a key has an underlying string value in the map[string]interface{} vars
// and safely returns the result
func (vars Vars) Get(key string) (string, error) {
	if vars[key] == nil {
		return "", fmt.Errorf("workflow var %s found nil", key)
	}

	value, ok := vars[key].(string)
	if !ok {
		return "", fmt.Errorf("workflow var %s found non-string type for the underlying value", key)
	}

	return value, nil
}
