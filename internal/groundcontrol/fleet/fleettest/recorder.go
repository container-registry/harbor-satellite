// Package fleettest has recording fakes for the fleet ports. A fake records
// each call in order, fails a method on request and returns what the test
// configured. Writes change nothing, so a test seeds every lookup it needs.
// Fakes that share one Recorder log into one sequence, which lets a test
// assert the order of calls across ports.
package fleettest

import "sync"

// Call is one recorded port call. Method is the port method, prefixed with
// the repository for repository calls ("Satellites.Create"); Args are the
// arguments after the context.
type Call struct {
	Method string
	Args   []any
}

// Recorder is safe for concurrent use; its zero value is ready to use.
type Recorder struct {
	mu    sync.Mutex
	calls []Call
	fails map[string]error
}

func NewRecorder() *Recorder {
	return &Recorder{}
}

// Record logs a call and returns the error injected for method, if any.
func (r *Recorder) Record(method string, args ...any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, Call{Method: method, Args: args})
	return r.fails[method]
}

// Note logs a call of a method that cannot fail, such as a buffered
// write; FailOn has no effect on it.
func (r *Recorder) Note(method string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, Call{Method: method, Args: args})
}

// FailOn makes every later call of method return err.
func (r *Recorder) FailOn(method string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fails == nil {
		r.fails = map[string]error{}
	}
	r.fails[method] = err
}

func (r *Recorder) ClearFaults() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fails = nil
}

func (r *Recorder) Calls() []Call {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Call(nil), r.calls...)
}

// Methods returns the recorded method names in call order.
func (r *Recorder) Methods() []string {
	calls := r.Calls()
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		out = append(out, c.Method)
	}
	return out
}

// CallsTo returns the calls of method in call order.
func (r *Recorder) CallsTo(method string) []Call {
	var out []Call
	for _, c := range r.Calls() {
		if c.Method == method {
			out = append(out, c)
		}
	}
	return out
}

func (r *Recorder) Count(method string) int {
	return len(r.CallsTo(method))
}

func orNew(rec *Recorder) *Recorder {
	if rec == nil {
		return NewRecorder()
	}
	return rec
}
