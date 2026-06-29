// SPDX-License-Identifier: Apache-2.0

package crons

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

// SystemHandler is an in-process maintenance task run by the runtime.
type SystemHandler func(ctx context.Context) error

// SystemScheduler runs named runtime maintenance jobs on cron expressions.
// It is separate from suite_crons: those rows enqueue customer-visible jobs,
// while system jobs execute local runtime housekeeping.
type SystemScheduler struct {
	mu       sync.Mutex
	jobs     map[string]*systemJob
	log      *slog.Logger
	interval time.Duration
}

type systemJob struct {
	name     string
	schedule string
	parsed   cron.Schedule
	next     time.Time
	handler  SystemHandler
}

func NewSystemScheduler(log *slog.Logger) *SystemScheduler {
	if log == nil {
		log = slog.Default()
	}
	return &SystemScheduler{
		jobs:     map[string]*systemJob{},
		log:      log,
		interval: time.Minute,
	}
}

func (s *SystemScheduler) RegisterSystem(name, schedule string, handler SystemHandler) error {
	if s == nil {
		return fmt.Errorf("%w: system scheduler is nil", ErrNotConfigured)
	}
	if name == "" {
		return fmt.Errorf("%w: system cron name is required", ErrInvalidInput)
	}
	if handler == nil {
		return fmt.Errorf("%w: system cron handler is required", ErrInvalidInput)
	}
	parsed, err := ParseSchedule(schedule)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs[name] = &systemJob{
		name:     name,
		schedule: schedule,
		parsed:   parsed,
		next:     parsed.Next(time.Now()),
		handler:  handler,
	}
	return nil
}

func (s *SystemScheduler) RunNow(ctx context.Context, name string) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	job := s.jobs[name]
	s.mu.Unlock()
	if job == nil {
		return ErrNotFound
	}
	return job.handler(ctx)
}

func (s *SystemScheduler) Run(ctx context.Context) {
	if s == nil {
		return
	}
	t := time.NewTicker(s.interval)
	defer t.Stop()
	s.log.Info("crons: system scheduler started", "interval", s.interval)
	for {
		select {
		case <-ctx.Done():
			s.log.Info("crons: system scheduler stopping")
			return
		case now := <-t.C:
			s.Tick(ctx, now)
		}
	}
}

func (s *SystemScheduler) Tick(ctx context.Context, now time.Time) int {
	if s == nil {
		return 0
	}
	type dueJob struct {
		name    string
		handler SystemHandler
	}
	due := []dueJob{}
	s.mu.Lock()
	for _, job := range s.jobs {
		if now.Before(job.next) {
			continue
		}
		due = append(due, dueJob{name: job.name, handler: job.handler})
		job.next = job.parsed.Next(now)
	}
	s.mu.Unlock()
	for _, job := range due {
		if err := job.handler(ctx); err != nil {
			s.log.Warn("crons: system job failed", "name", job.name, "error", err)
		}
	}
	return len(due)
}
