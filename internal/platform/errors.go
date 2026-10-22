package platform

// Failure keeps fixed protocol stages and numeric codes separate from the
// user-facing message. No URL query, Cookie or response body enters logs.
type Failure struct {
	Stage  string
	Code   int
	Cause  error
	Detail string
}

func (e *Failure) Error() string { return e.Cause.Error() }
func (e *Failure) Unwrap() error { return e.Cause }
func failure(stage string, code int, cause error) error {
	return &Failure{Stage: stage, Code: code, Cause: cause}
}
