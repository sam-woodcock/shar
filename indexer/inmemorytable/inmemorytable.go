package inmemorytable

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// Use the New() to create a new in memory table.
type Table struct {
	table map[string]*any
	sync.RWMutex
}

// New reates a new in memory table
func New() *Table {
	t := new(Table)

	t.table = make(map[string]*any)

	return t
}

// Add a new Key to the in memory table, and return a pointer to save the data
// If the key already exists, it will return the pointer to the current data to save/update the data
func (t *Table) Add(id string) *any {

	var (
		b bool
	)

	// Check if key already exists
	t.RLock()
	w, b := t.table[id]
	t.RUnlock()
	if b {
		return w
	}

	// Else create a new key
	w = new(interface{})
	t.Lock()
	defer t.Unlock() // defer just to be safer on a panic
	t.table[id] = w
	return w
}

// Remove an entry from the table
func (t *Table) Delete(id string) {
	// Check if key already exists
	t.RLock()
	delete(t.table, id)
	t.RUnlock()
}

// Get a pointer to memory location to save/update the data
// Returns nil,false if the location does not exist
func (t *Table) Get(id string) (w *any, b bool) {

	t.RLock()
	// Get the value and sucess
	w, b = t.table[id]
	t.RUnlock()
	return
}

// Drop the whole table
func (t *Table) Purge() {

	t.RLock()
	t.table = make(map[string]*any)
	t.RUnlock()
}

// @todo
// Returns a list of the current Keys
func (t *Table) List() map[string]*any {

	var (
		newList = make(map[string]*any)
		k       string
		v       *any
	)

	t.RLock()
	defer t.RUnlock()
	for k, v = range t.table {
		newList[k] = v
	}

	return newList
}

// Returns JSON of the whole table
func (t *Table) JSON() ([]byte, error) {

	// I am going to use defer, its possible json could crash leaving table locked.
	t.RLock()
	defer t.RUnlock()
	return json.MarshalIndent(t.table, "", "  ")
}

func (t *Table) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	t.RLock()
	defer t.RUnlock()

	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	b, e := t.JSON()

	if e != nil {
		fmt.Println(e)
		w.WriteHeader(502)
		return
	}

	w.Write(b)
}
