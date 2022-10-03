package subj

import (
	"fmt"
	"strings"
)

func NS(subj string, ns string) string {
	if strings.Contains(subj, "%s") {
		return fmt.Sprintf(subj, ns)
	}
	return subj
}
