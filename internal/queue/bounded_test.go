package queue_test

import (
	"errors"
	"testing"

	"github.com/lancekrogers/micbridge/internal/queue"
)

func TestBoundedOrderingAndDropNewest(t *testing.T) {
	q, err := queue.NewBounded[int](3, queue.DropNewest)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 3; i++ {
		if pushErr := q.Push(i); pushErr != nil {
			t.Fatal(pushErr)
		}
	}
	err = q.Push(99)
	var oe *queue.OverflowError
	if !errors.As(err, &oe) {
		t.Fatalf("expected overflow, got %v", err)
	}
	if q.OverflowCount() != 1 {
		t.Fatalf("overflow count = %d", q.OverflowCount())
	}
	if snap := q.Snapshot(); len(snap) != 3 || snap[0] != 1 || snap[2] != 3 {
		t.Fatalf("snapshot = %v", snap)
	}
	v, ok := q.Pop()
	if !ok || v != 1 {
		t.Fatalf("pop = %v %v", v, ok)
	}
}

func TestBoundedDropOldest(t *testing.T) {
	q, err := queue.NewBounded[string](2, queue.DropOldest)
	if err != nil {
		t.Fatal(err)
	}
	_ = q.Push("a")
	_ = q.Push("b")
	err = q.Push("c")
	if err == nil {
		t.Fatal("expected overflow error even when dropping oldest")
	}
	snap := q.Snapshot()
	if len(snap) != 2 || snap[0] != "b" || snap[1] != "c" {
		t.Fatalf("snapshot = %v", snap)
	}
}
