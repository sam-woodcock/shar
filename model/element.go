package model

import "fmt"

// Vars is a map of variables. The variables must be primitive go types.
type Vars map[string]any

// get takes the desired return type as parameter and safely searches the map and returns the value
// if it is found and is of the desired type.
func get[V string | int | int8 | int16 | int32 | int64 | byte | []byte | bool | float32 | float64](vars Vars, key string) (V, error) {
	// v is the return type value
	var v V

	if vars[key] == nil {
		return v, fmt.Errorf("workflow var %s found nil", key)
	}

	v, ok := vars[key].(V)
	if !ok {
		return v, fmt.Errorf("workflow var %s found unsupported type for the underlying value", key)
	}

	return v, nil
}

// GetString validates that a key has an underlying value in the map[string]interface{} vars
// and safely returns the result.
func (vars Vars) GetString(key string) (string, error) {
	return get[string](vars, key)
}

// GetInt validates that a key has an underlying value in the map[int]interface{} vars
// and safely returns the result.
func (vars Vars) GetInt(key string) (int, error) {
	return get[int](vars, key)
}

// GetInt8 validates that a key has an underlying value in the map[int]interface{} vars
// and safely returns the result.
func (vars Vars) GetInt8(key string) (int8, error) {
	return get[int8](vars, key)
}

// GetInt16 validates that a key has an underlying value in the map[int]interface{} vars
// and safely returns the result.
func (vars Vars) GetInt16(key string) (int16, error) {
	return get[int16](vars, key)
}

// GetInt32 validates that a key has an underlying value in the map[int]interface{} vars
// and safely returns the result.
func (vars Vars) GetInt32(key string) (int32, error) {
	return get[int32](vars, key)
}

// GetInt64 validates that a key has an underlying value in the map[int]interface{} vars
// and safely returns the result.
func (vars Vars) GetInt64(key string) (int64, error) {
	return get[int64](vars, key)
}

// GetByte validates that a key has an underlying value in the map[int]interface{} vars
// and safely returns the result.
func (vars Vars) GetByte(key string) ([]byte, error) {
	return get[[]byte](vars, key)
}

// GetBytes validates that a key has an underlying value in the map[int]interface{} vars
// and safely returns the result.
func (vars Vars) GetBytes(key string) ([]byte, error) {
	return get[[]byte](vars, key)
}

// GetBool validates that a key has an underlying value in the map[int]interface{} vars
// and safely returns the result.
func (vars Vars) GetBool(key string) (bool, error) {
	return get[bool](vars, key)
}

// GetFloat32 validates that a key has an underlying value in the map[int]interface{} vars
// and safely returns the result.
func (vars Vars) GetFloat32(key string) (float32, error) {
	return get[float32](vars, key)
}

// GetFloat64 validates that a key has an underlying value in the map[int]interface{} vars
// and safely returns the result.
func (vars Vars) GetFloat64(key string) (float64, error) {
	return get[float64](vars, key)
}
