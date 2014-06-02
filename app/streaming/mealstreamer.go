package streaming

import (
	"github.com/alexandre-normand/glukit/app/container"
	"github.com/alexandre-normand/glukit/app/glukitio"
	"github.com/alexandre-normand/glukit/app/model"
	"time"
)

type MealStreamer struct {
	head    *container.ImmutableList
	tailVal *model.Meal
	wr      glukitio.MealBatchWriter
	d       time.Duration
}

// NewMealStreamerDuration returns a new MealStreamer whose buffer has the specified size.
func NewMealStreamerDuration(wr glukitio.MealBatchWriter, bufferDuration time.Duration) *MealStreamer {
	return newMealStreamerDuration(nil, nil, wr, bufferDuration)
}

func newMealStreamerDuration(head *container.ImmutableList, tailVal *model.Meal, wr glukitio.MealBatchWriter, bufferDuration time.Duration) *MealStreamer {
	w := new(MealStreamer)
	w.head = head
	w.tailVal = tailVal
	w.wr = wr
	w.d = bufferDuration

	return w
}

// WriteMeal writes a single Meal into the buffer.
func (b *MealStreamer) WriteMeal(c model.Meal) (s *MealStreamer, err error) {
	return b.WriteMeals([]model.Meal{c})
}

// WriteMeals writes the contents of p into the buffer.
// It returns the number of bytes written.
// If nn < len(p), it also returns an error explaining
// why the write is short. p must be sorted by time (oldest to most recent).
func (b *MealStreamer) WriteMeals(p []model.Meal) (s *MealStreamer, err error) {
	s = newMealStreamerDuration(b.head, b.tailVal, b.wr, b.d)
	if err != nil {
		return s, err
	}

	for _, c := range p {
		t := c.GetTime()

		if s.head == nil {
			s = newMealStreamerDuration(container.NewImmutableList(nil, c), &c, s.wr, s.d)
		} else if t.Sub(s.tailVal.GetTime()) >= s.d {
			s, err = s.Flush()
			if err != nil {
				return s, err
			}
			s = newMealStreamerDuration(container.NewImmutableList(nil, c), &c, s.wr, s.d)
		} else {
			s = newMealStreamerDuration(container.NewImmutableList(s.head, c), s.tailVal, s.wr, s.d)
		}
	}

	return s, err
}

// Flush writes any buffered data to the underlying glukitio.Writer as a batch.
func (b *MealStreamer) Flush() (s *MealStreamer, err error) {
	r, size := b.head.ReverseList()
	batch := ListToArrayOfMealReads(r, size)

	if len(batch) > 0 {
		innerWriter, err := b.wr.WriteMealBatch(batch)
		if err != nil {
			return nil, err
		} else {
			return newMealStreamerDuration(nil, nil, innerWriter, b.d), nil
		}
	}

	return newMealStreamerDuration(nil, nil, b.wr, b.d), nil
}

func ListToArrayOfMealReads(head *container.ImmutableList, size int) []model.Meal {
	r := make([]model.Meal, size)
	cursor := head
	for i := 0; i < size; i++ {
		r[i] = cursor.Value().(model.Meal)
		cursor = cursor.Next()
	}

	return r
}

// Close flushes the buffer and the inner writer to effectively ensure nothing is left
// unwritten
func (b *MealStreamer) Close() (s *MealStreamer, err error) {
	g, err := b.Flush()
	if err != nil {
		return g, err
	}

	innerWriter, err := g.wr.Flush()
	if err != nil {
		return newMealStreamerDuration(g.head, g.tailVal, innerWriter, b.d), err
	}

	return g, nil
}