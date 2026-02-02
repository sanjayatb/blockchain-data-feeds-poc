package extract

import "testing"

func TestChunkerAdaptive(t *testing.T) {
	c := NewChunker(10, 1000, 200)
	if got := c.Current(); got != 200 {
		t.Fatalf("expected 200 got %d", got)
	}
	c.OnError()
	if got := c.Current(); got != 100 {
		t.Fatalf("expected 100 got %d", got)
	}
	c.OnSuccess()
	if got := c.Current(); got <= 100 {
		t.Fatalf("expected growth got %d", got)
	}
	// grow to max
	for i := 0; i < 50; i++ {
		c.OnSuccess()
	}
	if got := c.Current(); got != 1000 {
		t.Fatalf("expected max 1000 got %d", got)
	}
	// shrink to min
	for i := 0; i < 50; i++ {
		c.OnError()
	}
	if got := c.Current(); got != 10 {
		t.Fatalf("expected min 10 got %d", got)
	}
}
