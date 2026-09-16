package scheduler

import "time"

// ResetState must be called under the shared exclusive maintenance lease.
// Epoch-bound queue entries cannot start even if the loop already received one.
func (s *Scheduler) ResetState() {
	s.mu.Lock()
	s.status = Status{State: StateIdle}
	clear(s.sourceSchedules)
	clear(s.sourceRequests)
	s.mu.Unlock()
	s.progress.reset(time.Time{})
	select {
	case <-s.triggerCh:
	default:
	}
	select {
	case <-s.recomputeCh:
	default:
	}
	select {
	case <-s.sourceTriggerCh:
	default:
	}
	select {
	case <-s.scheduleCh:
	default:
	}
	select {
	case s.resetScheduleCh <- struct{}{}:
	default:
	}
}
