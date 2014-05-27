package store_test

import (
	"appengine/aetest"
	"github.com/alexandre-normand/glukit/app/model"
	. "github.com/alexandre-normand/glukit/app/store"
	"testing"
	"time"
)

func TestSimpleWriteOfSingleMealBatch(t *testing.T) {
	meals := make([]model.Meal, 25)
	ct, _ := time.Parse("02/01/2006 15:04", "18/04/2014 00:00")
	for i := 0; i < 25; i++ {
		readTime := ct.Add(time.Duration(i) * time.Hour)
		meals[i] = model.Meal{model.Time{readTime.Unix(), "America/Los_Angeles"}, float32(i), 0., 0., 0.}
	}

	c, err := aetest.NewContext(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	key := GetUserKey(c, "test@glukit.com")

	w := NewDataStoreMealBatchWriter(c, key)
	if _, err = w.WriteMealBatch(meals); err != nil {
		t.Fatal(err)
	}
}

func TestSimpleWriteOfMealBatches(t *testing.T) {
	b := make([]model.DayOfMeals, 10)
	ct, _ := time.Parse("02/01/2006 15:04", "18/04/2014 00:00")

	for i := 0; i < 10; i++ {
		meals := make([]model.Meal, 24)
		for j := 0; j < 24; j++ {
			readTime := ct.Add(time.Duration(i*24+j) * time.Hour)
			meals[j] = model.Meal{model.Time{readTime.Unix(), "America/Los_Angeles"}, float32(i*24 + j), 0., 0., 0.}
		}
		b[i] = model.DayOfMeals{meals}
	}

	c, err := aetest.NewContext(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	key := GetUserKey(c, "test@glukit.com")

	w := NewDataStoreMealBatchWriter(c, key)
	if _, err = w.WriteMealBatches(b); err != nil {
		t.Fatal(err)
	}
}
