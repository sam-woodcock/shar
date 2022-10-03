package services

type publishOptions struct {
	Embargo int
	ID      string
}

type PublishOpt interface {
	Apply(n *publishOptions)
}

type publishEmbargoOption struct {
	value int
}

func (o *publishEmbargoOption) Apply(n *publishOptions) { n.Embargo = o.value }

func WithEmbargo(embargo int) *publishEmbargoOption {
	return &publishEmbargoOption{value: embargo}
}
