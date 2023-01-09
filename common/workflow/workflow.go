package workflow

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"gitlab.com/shar-workflow/shar/model"
)

// GetHash - will return a hash of a workflow definition without the GZipped source field/
func GetHash(wf *model.Workflow) ([]byte, error) {
	b2 := wf.GzipSource
	wf.GzipSource = nil
	defer func(wf *model.Workflow) {
		wf.GzipSource = b2
	}(wf)
	b, err := json.Marshal(wf)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal the workflow definition: %w", err)
	}
	h := sha256.New()
	if _, err := h.Write(b); err != nil {
		return nil, fmt.Errorf("could not write the workflow definitino to the hash provider: %s", wf.Name)
	}
	hash := h.Sum(nil)
	return hash, nil
}
