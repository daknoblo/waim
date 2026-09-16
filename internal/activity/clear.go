package activity

// Clear drops retained user labels without allowing any old run handle to
// revive them. Called under application maintenance admission.
func (t *Tracker) Clear() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for job, v := range t.slots {
		v.token++
		v.state = State{Job: job, Status: Idle}
		t.slots[job] = v
	}
}
