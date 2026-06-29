// SPDX-License-Identifier: Apache-2.0

package logger

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestRingSubscribeFanout(t *testing.T) {
	r := NewRing(8, "runtime")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := r.Subscribe(ctx)
	b := r.Subscribe(ctx)
	r.Append(Line{Level: LevelInfo, Msg: "hello"})
	for name, ch := range map[string]<-chan Line{"a": a, "b": b} {
		select {
		case line := <-ch:
			if line.Msg != "hello" {
				t.Fatalf("%s got msg %q", name, line.Msg)
			}
		case <-time.After(time.Second):
			t.Fatalf("%s did not receive fanout line", name)
		}
	}
}

func TestRingSubscribeCancelUnsubscribes(t *testing.T) {
	r := NewRing(8, "runtime")
	ctx, cancel := context.WithCancel(context.Background())
	ch := r.Subscribe(ctx)
	cancel()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("subscription channel stayed open after cancel")
		}
	case <-time.After(time.Second):
		t.Fatal("subscription did not close after cancel")
	}
}

func TestRingSubscribeSlowConsumerDropOldest(t *testing.T) {
	r := NewRing(8, "runtime")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := r.Subscribe(ctx)
	for i := 0; i < defaultSubscriberBuffer+20; i++ {
		r.Append(Line{Level: LevelInfo, Msg: strconv.Itoa(i)})
	}
	var last string
	for {
		select {
		case line := <-ch:
			last = line.Msg
		default:
			if last != strconv.Itoa(defaultSubscriberBuffer+19) {
				t.Fatalf("slow subscriber did not retain newest line; last=%q", last)
			}
			return
		}
	}
}

func TestRingConcurrentAppendSubscribe(t *testing.T) {
	r := NewRing(64, "runtime")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				r.Append(Line{Level: LevelInfo, Msg: strconv.Itoa(id*1000 + j)})
			}
		}(i)
	}
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch := r.Subscribe(ctx)
			select {
			case <-ch:
			case <-time.After(time.Second):
			}
		}()
	}
	wg.Wait()
}
