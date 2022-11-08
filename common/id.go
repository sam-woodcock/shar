package common

type TrackingID []string

func (t TrackingID) ID() string {
	gen := len(t)
	if gen > 0 {
		return t[gen-1]
	}
	return ""
}

func (t TrackingID) ParentID() string {
	return t.Ancestor(1)
}

func (t TrackingID) Ancestor(gen int) string {
	if len(t) > 0 && len(t)-1-gen >= 0 {
		return t[len(t)-1-gen]
	}
	return ""
}

func (t TrackingID) Pop() TrackingID {
	c := make([]string, len(t))
	copy(c, t)
	if len(c)-1 >= 0 {
		c = c[:len(c)-1]
	}
	return c
}

func (t TrackingID) Push(id string) TrackingID {
	c := make([]string, len(t), len(t)+1)
	copy(c, t)
	c = append(c, id)
	return c
}
