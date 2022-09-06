package subj

import (
	"fmt"
	"strings"
)

func GetSubjectNS(subject string) string {
	d := strings.Index(subject, ".") + 1
	return subject[d : strings.Index(subject[d:], ".")+d]
}

func SubjNS(subj string, ns string) string {
	if strings.Contains(subj, "%s") {
		return fmt.Sprintf(subj, ns)
	}
	return subj
}
