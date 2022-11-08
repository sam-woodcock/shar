package services

type publishOptions struct {
	Embargo int
	ID      string
}

type PublishOpt interface {
	Apply(n *publishOptions)
}

type PublishEmbargoOption struct {
	value int
}

func (o *PublishEmbargoOption) Apply(n *publishOptions) { n.Embargo = o.value }

func WithEmbargo(embargo int) *PublishEmbargoOption {
	return &PublishEmbargoOption{value: embargo}
}
