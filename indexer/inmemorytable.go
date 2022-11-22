package main

import (
	"encoding/json"
	"sync"
)

// Use the New() to create a new in memory table.
type inMemoryTable struct {
	table any
	sync.RWMutex
}

// Creates a new in memory table
func New() *inMemoryTable {
	t := new(inMemoryTable)

	t.table = make(map[string]*any)

	return t
}

// Add a new Key to the in memory table, and return a pointer to save the data
// If the key already exists, it will return the pointer to the current data to save/update the data
func (t *inMemoryTable) Add(id string) *any {

	var (
		b bool
	)

	// Check if key already exists
	t.RLock()
	w, b := t.table.(map[string]*any)[id]
	t.RUnlock()
	if b {
		return w
	}

	// Else create a new key
	w = new(interface{})
	t.Lock()
	defer t.Unlock() // defer just to be safer on a panic
	t.table.(map[string]*any)[id] = w
	return w
}

// Get a pointer to memory location to save/update the data
// Returns nil,false if the location does not exist
func (t *inMemoryTable) Get(id string) (w *any, b bool) {

	t.RLock()
	// Get the value and sucess
	w, b = t.table.(map[string]*any)[id]
	t.RUnlock()
	return
}

// TBC
// Returns a list of the current Keys
func (t *inMemoryTable) List() *any {
	return nil
}

// Returns JSON of the whole table
func (t *inMemoryTable) JSON() ([]byte, error) {

	// I am going to use defer, its possible json could crash leaving table locked.
	t.RLock()
	defer t.RUnlock()
	return json.MarshalIndent(t.table, "", "  ")
}
