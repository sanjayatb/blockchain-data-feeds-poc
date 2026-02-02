package extract

type Chunker struct {
	min     uint64
	max     uint64
	current uint64
}

func NewChunker(min, max, start uint64) *Chunker {
	if min == 0 {
		min = 1
	}
	if max < min {
		max = min
	}
	if start < min {
		start = min
	}
	if start > max {
		start = max
	}
	return &Chunker{min: min, max: max, current: start}
}

func (c *Chunker) Current() uint64 {
	return c.current
}

func (c *Chunker) OnError() {
	if c.current <= c.min {
		return
	}
	next := c.current / 2
	if next < c.min {
		next = c.min
	}
	c.current = next
}

func (c *Chunker) OnSuccess() {
	if c.current >= c.max {
		return
	}
	grow := c.current/10 + 1
	next := c.current + grow
	if next > c.max {
		next = c.max
	}
	c.current = next
}
