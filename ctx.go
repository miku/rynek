package rynek

// Ctx is the ambient context threaded to every task builder. It bundles the run
// parameters (embedded Params) with project-level defaults such as the output
// extension, so a builder can read parameters from it, construct its
// dependencies with it, and wrap its Shell through it -- instead of repeating
// the parameters and the extension in every task literal.
//
// A builder therefore takes a Ctx rather than raw Params:
//
//	func Tokens(c rynek.Ctx) rynek.Task {
//		return c.Shell(rynek.Shell{
//			Name: "Tokens",
//			In:   rynek.Inputs{"in": Corpus{c}},
//			Cmd:  `tr 'A-Z ' 'a-z\n' < {in} | grep -v '^$' > {out}`,
//		})
//	}
type Ctx struct {
	Params
	Ext string // default output extension for shells built via Shell
}

// Shell applies the ambient defaults to s and returns it: it fills in the
// parameters used for the conventional output path, and -- unless the task set
// its own -- the project default extension. What remains in the task literal is
// the essential content: Name, In, Cmd.
func (c Ctx) Shell(s Shell) Shell {
	s.P = c.Params
	if s.Ext == "" {
		s.Ext = c.Ext
	}
	return s
}
