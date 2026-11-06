package fv

type cnt struct{ N, M int }

func (s *cnt) Clone() State { c := *s; return &c }

func n(s State) int { return s.(*cnt).N }

func act(name string, f func(c *Choice, s *cnt) bool) Action {
	return Action{Name: name, Step: func(c *Choice, s State) State {
		if !f(c, s.(*cnt)) {
			return nil
		}
		return s
	}}
}

func counter(limit int) Model {
	return Model{
		Name: "counter",
		Init: func() []State { return []State{&cnt{}} },
		Actions: []Action{act("inc", func(c *Choice, s *cnt) bool {
			if s.N >= limit {
				return false
			}
			s.N += 1 + c.Pick(2)
			return true
		})},
	}
}
