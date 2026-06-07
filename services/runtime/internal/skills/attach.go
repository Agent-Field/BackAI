// attach.go — thin convenience wrappers over Store.{Attach,Detach}.
//
// These exist as standalone helpers (rather than methods on Store)
// so the REST layer can use the same surface the SDK would — and
// future loops (background reattach on missing agents, batched
// detach on uninstall) have a natural home.
package skills

import "context"

// Attach binds a skill onto an agent. Idempotent — see Store.Attach
// for the contract.
func Attach(ctx context.Context, s *Store, skillID, agent string) error {
	if s == nil {
		return ErrNotConfigured
	}
	return s.Attach(ctx, skillID, agent)
}

// Detach removes one (skill_id, agent) pair. Returns ErrNotFound if
// no row matched.
func Detach(ctx context.Context, s *Store, skillID, agent string) error {
	if s == nil {
		return ErrNotConfigured
	}
	return s.Detach(ctx, skillID, agent)
}
