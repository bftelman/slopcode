package picker

import (
	"strings"
	"time"

	"github.com/sahilm/fuzzy"
)

// MaxRows caps how many ranked rows cross the channel and reach the renderer.
// The overlay only ever shows a screenful; Result.Total still reports the true
// match count so the counter stays honest.
const MaxRows = 200

// defaultDebounce is the idle delay before a query is ranked. Ranking 200k
// candidates costs on the order of 200 ms, so collapsing a burst of keystrokes
// into one pass is what keeps typing responsive.
const defaultDebounce = 25 * time.Millisecond

// Row is one ranked candidate plus the offsets that matched.
type Row struct {
	Cand    Candidate
	Matched []int // offsets into Cand.Text; nil when the query is empty
	Score   int
}

// Result is a completed ranking pass, tagged for staleness.
type Result struct {
	// Gen echoes the generation the caller passed to Open. Staleness is the
	// caller's to judge - it owns the counter, the way the editor owns
	// docVersion for completion - because only the caller knows which picker is
	// currently on screen when a late result arrives.
	Gen   int
	Query string
	Rows  []Row // truncated to MaxRows
	Total int   // total matches before truncation
	Err   error // non-nil leaves the list empty; never fatal

	// Loading reports that a listing is still in flight, so the rows are either
	// empty or a cached set from a previous open. The UI uses it to distinguish
	// "still working" from "genuinely no matches".
	Loading bool
}

type cmdKind int

const (
	cmdOpen cmdKind = iota
	cmdQuery
	cmdLoaded
	cmdClose
)

type command struct {
	kind  cmdKind
	src   Source
	gen   int
	q     string
	cands []Candidate
	err   error
	done  chan struct{}
}

// Engine ranks candidates on its own goroutine: debounced, superseded by newer
// queries, and generation-tagged so the UI can drop stale passes. Every exported
// method returns immediately.
//
// Ranking must not happen on the UI thread. Measured with sahilm/fuzzy, one pass
// over 200k candidates takes ~200 ms, which would visibly stall the editor for a
// buffer-lines picker on a large file or a file picker in a big repository.
type Engine struct {
	debounce time.Duration
	cmds     chan command
	results  chan Result

	// cache holds previously listed candidates by Source.Key, so reopening a
	// picker serves instantly while a refresh runs behind it. Owned by the loop
	// goroutine; never touched from outside it.
	cache map[string][]Candidate

	// quit is closed when the loop stops, so an in-flight loader goroutine can
	// abandon its send instead of blocking forever on a channel nobody reads.
	quit chan struct{}
}

// Option configures an Engine.
type Option func(*Engine)

// WithDebounce sets the idle delay before a query is ranked.
func WithDebounce(d time.Duration) Option { return func(e *Engine) { e.debounce = d } }

// NewEngine builds and starts an Engine.
func NewEngine(opts ...Option) *Engine {
	e := &Engine{
		debounce: defaultDebounce,
		cmds:     make(chan command, 8),
		results:  make(chan Result, 1),
		cache:    map[string][]Candidate{},
		quit:     make(chan struct{}),
	}
	for _, o := range opts {
		o(e)
	}
	go e.loop()
	return e
}

// Results delivers completed ranking passes. Each carries the generation it ran
// against; consumers drop results from an older generation.
func (e *Engine) Results() <-chan Result { return e.results }

// Open loads a source's candidates and emits the unfiltered list. Every Result
// from then until the next Open carries gen, so the caller can drop results
// belonging to a picker it has already closed or replaced.
func (e *Engine) Open(gen int, src Source) {
	e.cmds <- command{kind: cmdOpen, src: src, gen: gen}
}

// Query re-ranks after the debounce interval, superseding any pending pass.
// Non-blocking: dropping a send is safe because a later keystroke supersedes it.
func (e *Engine) Query(q string) {
	select {
	case e.cmds <- command{kind: cmdQuery, q: q}:
	default:
	}
}

// Close stops the engine and closes Results.
func (e *Engine) Close() error {
	done := make(chan struct{})
	e.cmds <- command{kind: cmdClose, done: done}
	<-done
	return nil
}

// emit publishes a result, keeping only the freshest one. It drops an unread
// older result rather than blocking the ranking loop: results supersede each
// other, so the newest is the only one worth delivering, and a blocking send
// would let a UI that has stopped reading wedge the engine.
func (e *Engine) emit(res Result) {
	select {
	case <-e.results:
	default:
	}
	select {
	case e.results <- res:
	default:
	}
}

// subset presents a chosen set of indexes into all as a fuzzy.Source, so a
// longer query can be scored against only the previous pass's survivors.
type subset struct {
	all []Candidate
	idx []int
}

func (s subset) String(i int) string { return s.all[s.idx[i]].Text }
func (s subset) Len() int            { return len(s.idx) }

// candSource adapts a full candidate slice to fuzzy.Source.
type candSource []Candidate

func (c candSource) String(i int) string { return c[i].Text }
func (c candSource) Len() int            { return len(c) }

func (e *Engine) loop() {
	var (
		cands   []Candidate
		loadErr error
		gen     int
		query   string

		// Narrowing state: the query that lastIdx belongs to, and the indexes
		// into cands that matched it.
		lastQuery string
		lastIdx   []int

		timer *time.Timer
		fire  = make(chan struct{}, 1)
	)
	arm := func() {
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(e.debounce, func() {
			select {
			case fire <- struct{}{}:
			default:
			}
		})
	}

	for {
		select {
		case c := <-e.cmds:
			switch c.kind {
			case cmdOpen:
				gen = c.gen
				query = ""
				key := c.src.Key()

				// Serve a previously listed set for this source immediately, so
				// reopening a picker is instant instead of paying the listing cost
				// again. Listing a project measured ~200 ms on Windows, almost all
				// of it subprocess overhead.
				cands, loadErr = nil, nil
				if key != "" {
					if cached, ok := e.cache[key]; ok {
						cands = cached
					}
				}
				rows, idx := rank(cands, "", nil)
				lastQuery, lastIdx = "", idx
				e.emit(Result{Gen: gen, Rows: rows, Total: len(idx), Loading: true})

				// Load (or refresh) off this goroutine, so the loop keeps handling
				// keystrokes while an external lister runs. The result comes back
				// as cmdLoaded and is discarded if the generation moved on.
				go func(src Source, forGen int) {
					loaded, err := src.Candidates()
					select {
					case e.cmds <- command{kind: cmdLoaded, gen: forGen, cands: loaded, err: err, src: src}:
					case <-e.quit: // engine closed while we were listing
					}
				}(c.src, gen)
			case cmdLoaded:
				if c.gen != gen {
					continue // a picker that has since been closed or replaced
				}
				if c.err != nil && len(cands) > 0 {
					// A refresh failed but we are already showing a cached list;
					// keep it rather than replacing good data with an error.
					continue
				}
				cands, loadErr = c.cands, c.err
				if key := c.src.Key(); key != "" && c.err == nil {
					e.cache[key] = c.cands
				}
				rows, idx := rank(cands, query, nil)
				lastQuery, lastIdx = query, idx
				e.emit(Result{Gen: gen, Query: query, Rows: rows, Total: len(idx), Err: loadErr})
			case cmdQuery:
				query = c.q
				arm()
			case cmdClose:
				if timer != nil {
					timer.Stop()
				}
				close(e.results)
				close(c.done)
				return
			}
		case <-fire:
			// Narrow only when the query extends the one lastIdx belongs to.
			// Any other edit - backspace, retype, paste - needs a full rescan,
			// because removing characters can bring back candidates that the
			// previous, longer query had rejected.
			var prev []int
			if lastQuery != "" && query != lastQuery && strings.HasPrefix(query, lastQuery) {
				prev = lastIdx
			}
			rows, idx := rank(cands, query, prev)
			lastQuery, lastIdx = query, idx
			e.emit(Result{Gen: gen, Query: query, Rows: rows, Total: len(idx), Err: loadErr})
		}
	}
}

// rank scores cands against query and returns the display rows (capped at
// MaxRows) plus every matching index into cands, for the next narrowing pass.
//
// When prev is non-nil only those indexes are scored, and results are mapped
// back to cands. An empty query is special-cased: fuzzy.Find("") returns zero
// matches, so without this an freshly-opened picker would show an empty list.
func rank(cands []Candidate, query string, prev []int) ([]Row, []int) {
	if query == "" {
		idx := allIndexes(len(cands))
		rows := make([]Row, 0, min(len(cands), MaxRows))
		for i := 0; i < len(cands) && i < MaxRows; i++ {
			rows = append(rows, Row{Cand: cands[i]})
		}
		return rows, idx
	}

	var matches fuzzy.Matches
	mapIdx := func(i int) int { return i }
	if prev != nil {
		matches = fuzzy.FindFrom(query, subset{all: cands, idx: prev})
		mapIdx = func(i int) int { return prev[i] }
	} else {
		matches = fuzzy.FindFrom(query, candSource(cands))
	}

	idx := make([]int, 0, len(matches))
	rows := make([]Row, 0, min(len(matches), MaxRows))
	for i, m := range matches {
		at := mapIdx(m.Index)
		idx = append(idx, at)
		if i < MaxRows {
			rows = append(rows, Row{Cand: cands[at], Matched: m.MatchedIndexes, Score: m.Score})
		}
	}
	return rows, idx
}

func allIndexes(n int) []int {
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	return idx
}
