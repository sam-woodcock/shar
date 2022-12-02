package subj

import (
	"fmt"
	"strings"
)

// NS returns a subject with the placeholder replaced with the namespace
func NS(subj string, ns string) string {
	if strings.Contains(subj, "%s") {
		return fmt.Sprintf(subj, ns)
	}
	return subj
}
